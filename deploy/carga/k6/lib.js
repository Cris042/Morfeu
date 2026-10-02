// Biblioteca comum dos cenários de carga (PRD 0045 RF09). Open model: todos os
// cenários usam executores de taxa de chegada (constant-arrival-rate /
// ramping-arrival-rate) — a taxa NÃO desacelera quando o servidor degrada, e
// iterações que não couberem nos VUs aparecem em dropped_iterations (run inválido).
// Os thresholds aqui são guarda-corpo do gerador; o veredito do SLO é
// server-side (Prometheus — docs/carga/queries.md).
import http from 'k6/http';
import exec from 'k6/execution';
import { check } from 'k6';
import { Counter, Trend } from 'k6/metrics';

// --- Alvo: só o compose local / rede privada. Nunca a URL pública. ---------
const BASE_URL = (__ENV.BASE_URL || 'http://app:8080').replace(/\/+$/, '');

// Aceita: serviço do compose (sem ponto), localhost, loopback, redes privadas
// (10/8, 172.16/12, 192.168/16) e host.docker.internal. Só http (o alvo local
// não tem TLS; https implicaria um host público).
function hostPermitido(url) {
  const m = /^http:\/\/([^/:?#@]+)(?::\d+)?(?:[/?#]|$)/.exec(url);
  if (!m) return false;
  const h = m[1].toLowerCase();
  if (!h.includes('.')) return true; // 'app', 'localhost'
  if (h === 'host.docker.internal') return true;
  const ip = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(h);
  if (!ip) return false;
  const [a, b] = [Number(ip[1]), Number(ip[2])];
  return a === 127 || a === 10 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168);
}
if (!hostPermitido(BASE_URL)) {
  throw new Error(`BASE_URL recusada (${BASE_URL}): o teste de carga só mira o compose local ou rede privada, nunca uma URL pública`);
}
export const alvo = BASE_URL;

// --- Parâmetros por env (padrões = smoke de ~20 s). ------------------------
export const TAXA = Number(__ENV.TAXA || 10); // req/s (ou fluxos/s) por cenário
export const DURACAO = __ENV.DURACAO || '20s';
export const VUS_PRE = Number(__ENV.VUS_PRE || 50);
export const VUS_MAX = Number(__ENV.VUS_MAX || 200);
const SESSOES_POR_FILME = Number(__ENV.SESSOES_POR_FILME || 5);
const MAX_SESSOES = Number(__ENV.MAX_SESSOES || 200);

// --- Métricas próprias. -----------------------------------------------------
export const fluxoCheckout = new Trend('checkout_fluxo_duration', true);
export const fluxosOk = new Counter('checkout_fluxos_ok');
export const assentoOcupado = new Counter('checkout_assento_ocupado');

const CABECALHOS_JSON = { 'Content-Type': 'application/json', 'X-Requested-With': 'morfeu' };
const SO_2XX = http.expectedStatuses({ min: 200, max: 299 });
http.setResponseCallback(SO_2XX);

// Guarda-corpo comum (run inválido se o gerador não sustentar a taxa).
export function thresholds(extra = {}) {
  return Object.assign(
    {
      http_req_failed: ['rate<0.01'],
      dropped_iterations: ['count==0'],
      checks: ['rate>0.999'],
    },
    extra,
  );
}

// Cenário de taxa constante: `taxa` iterações por `timeUnit`.
export function constante(funcao, taxa = TAXA, timeUnit = '1s', duracao = DURACAO) {
  return {
    executor: 'constant-arrival-rate',
    exec: funcao,
    rate: taxa,
    timeUnit,
    duration: duracao,
    preAllocatedVUs: VUS_PRE,
    maxVUs: VUS_MAX,
  };
}

// --- setup(): descobre sessões e assentos pela API pública. ----------------
function lerJSON(url) {
  const r = http.get(url, { responseType: 'text', tags: { name: 'setup' } });
  if (r.status !== 200) throw new Error(`setup: ${url} -> ${r.status} (stack no ar e seed rodado?)`);
  return r.json();
}

export function descobrir() {
  const filmes = lerJSON(`${BASE_URL}/filmes`);
  const sessoes = [];
  for (const f of filmes) {
    const lista = lerJSON(`${BASE_URL}/filmes/${f.id}/sessoes`);
    for (const s of lista.slice(0, SESSOES_POR_FILME)) sessoes.push(s.id);
    if (sessoes.length >= MAX_SESSOES) break;
  }
  if (sessoes.length === 0) throw new Error('setup: nenhuma sessão futura — rode deploy/carga/seed.sql');
  const mapa = lerJSON(`${BASE_URL}/sessoes/${sessoes[0]}/mapa`);
  const assentos = mapa.assentos.map((a) => a.codigo);
  // Deslocamento aleatório por run: runs consecutivos não reusam os mesmos assentos.
  const base = Math.floor(Math.random() * sessoes.length * assentos.length);
  return { sessoes, assentos, filmes: filmes.map((f) => f.id), base };
}

// --- Leitura (cartaz, sessões do filme, ocupação). --------------------------
function escolher(lista) {
  return lista[Math.floor(Math.random() * lista.length)];
}

// Mistura de leitura: ocupação é o polling do mapa (o caminho mais quente).
export function leituraAleatoria(dados) {
  const dado = Math.random();
  let r;
  if (dado < 0.2) {
    r = http.get(`${BASE_URL}/filmes`, { tags: { name: 'GET /filmes' } });
  } else if (dado < 0.5) {
    r = http.get(`${BASE_URL}/filmes/${escolher(dados.filmes)}/sessoes`, { tags: { name: 'GET /filmes/:id/sessoes' } });
  } else {
    r = http.get(`${BASE_URL}/sessoes/${escolher(dados.sessoes)}/ocupacao`, { tags: { name: 'GET /sessoes/:id/ocupacao' } });
  }
  check(r, { 'leitura 200': (x) => x.status === 200 });
}

// --- Escrita: trava, pedido e pagamento de teste. ---------------------------
// Assento determinístico para o g-ésimo uso do run: espalha por sessões antes
// de avançar de assento (evita concentrar numa sessão só).
export function assentoDoIndice(dados, g) {
  const s = dados.sessoes.length;
  const n = dados.assentos.length;
  return { sessaoId: dados.sessoes[g % s], assento: dados.assentos[Math.floor(g / s) % n] };
}

// Requisição de trava no formato do http.batch: [método, url, corpo, params].
export function requisicaoTravar(sessaoId, assentos, params = {}) {
  return [
    'POST',
    `${BASE_URL}/sessoes/${sessaoId}/holds`,
    JSON.stringify({ assentos }),
    Object.assign({ headers: CABECALHOS_JSON, tags: { name: 'POST /sessoes/:id/holds' } }, params),
  ];
}

export function travar(sessaoId, assentos, params = {}) {
  const [, url, corpo, p] = requisicaoTravar(sessaoId, assentos, params);
  return http.post(url, corpo, p);
}

// O cookie do carrinho é Secure: o jar do k6 não o devolve por http, então o
// header Cookie é montado à mão a partir do Set-Cookie.
function cookieDoCarrinho(res) {
  const c = res.cookies && res.cookies.morfeu_carrinho;
  return c && c.length ? `morfeu_carrinho=${c[0].value}` : '';
}

// Fluxo completo: trava → pedido → pagamento de teste → confere pedido pago.
// `g` é o índice global do uso (único no cenário). Tenta até 3 assentos se o
// escolhido já estiver ocupado de runs anteriores (contado em assentoOcupado).
export function checkoutCompleto(dados, g) {
  const inicio = Date.now();
  let sessaoId;
  let assento;
  let trava;
  for (let t = 0; t < 3; t++) {
    ({ sessaoId, assento } = assentoDoIndice(dados, g + t * 40009));
    trava = travar(sessaoId, [assento], { responseType: 'text', responseCallback: http.expectedStatuses(201, 409) });
    if (trava.status !== 409) break;
    assentoOcupado.add(1);
  }
  const ok201 = check(trava, { 'trava 201': (r) => r.status === 201 });
  if (!ok201) return false;

  const cookie = cookieDoCarrinho(trava);
  const sessao = { headers: Object.assign({ Cookie: cookie }, CABECALHOS_JSON), responseType: 'text' };
  const email = `carga-${exec.vu.idInTest}-${exec.vu.iterationInScenario}@example.test`;
  const pedido = http.post(
    `${BASE_URL}/pedidos`,
    JSON.stringify({ email, sessao_id: sessaoId, assentos: [assento] }),
    Object.assign({ tags: { name: 'POST /pedidos' } }, sessao),
  );
  if (!check(pedido, { 'pedido 201': (r) => r.status === 201 })) return false;
  const pedidoId = pedido.json('pedido.id');

  const pago = http.post(`${BASE_URL}/__teste/pagar/${pedidoId}`, null, {
    responseType: 'text',
    tags: { name: 'POST /__teste/pagar/:id' },
  });
  if (!check(pago, { 'pagamento 204': (r) => r.status === 204 })) return false;

  const lido = http.get(`${BASE_URL}/pedidos/${pedidoId}`, Object.assign({ tags: { name: 'GET /pedidos/:id' } }, sessao));
  const pagoOk = check(lido, { 'pedido pago': (r) => r.status === 200 && r.json('status') === 'pago' });
  if (pagoOk) {
    fluxosOk.add(1);
    fluxoCheckout.add(Date.now() - inicio);
  }
  return pagoOk;
}
