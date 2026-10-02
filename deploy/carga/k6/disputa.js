// Disputa de assento (PRD 0045 RF09): a cada iteração, DISPUTANTES requisições
// PARALELAS (http.batch) tentam travar o MESMO assento, cada uma sem carrinho
// (donos distintos). Esperado por assento livre: exatamente 1× 201 e N−1× 409;
// zero 5xx. TAXA = assentos disputados por segundo. Assento já ocupado de run
// anterior = 0 vencedor (contado, não é erro); 2+ vencedores = violação.
import exec from 'k6/execution';
import http from 'k6/http';
import { check } from 'k6';
import { Counter } from 'k6/metrics';
import { descobrir, assentoDoIndice, requisicaoTravar, thresholds, constante, TAXA } from './lib.js';

const DISPUTANTES = Number(__ENV.DISPUTANTES || 20);
const vencedores = new Counter('disputa_vencedores');
const semVencedor = new Counter('disputa_assentos_sem_vencedor');
const multiplosVencedores = new Counter('disputa_assentos_com_mais_de_um_vencedor');

export const options = {
  batchPerHost: DISPUTANTES, // o padrão (6) serializaria a disputa
  scenarios: { disputa: constante('disputa', TAXA) },
  thresholds: thresholds({ disputa_assentos_com_mais_de_um_vencedor: ['count==0'] }),
};

export function setup() {
  return descobrir();
}

export function disputa(dados) {
  const { sessaoId, assento } = assentoDoIndice(dados, dados.base + exec.scenario.iterationInTest);
  const lote = [];
  for (let i = 0; i < DISPUTANTES; i++) {
    lote.push(requisicaoTravar(sessaoId, [assento], { responseCallback: http.expectedStatuses(201, 409) }));
  }
  const respostas = http.batch(lote);
  const ganhou = respostas.filter((r) => r.status === 201).length;
  const perdeu = respostas.filter((r) => r.status === 409).length;
  vencedores.add(ganhou);
  if (ganhou === 0) semVencedor.add(1);
  if (ganhou > 1) multiplosVencedores.add(1);
  check(respostas, {
    'só 201 ou 409 (zero 5xx)': () => ganhou + perdeu === DISPUTANTES,
    'no máximo um vencedor': () => ganhou <= 1,
  });
}
