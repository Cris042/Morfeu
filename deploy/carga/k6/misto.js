// Misto 90/10 (PRD 0045 RF09): 90% leituras e 10% checkouts completos, em
// DUAS taxas de chegada independentes (cada uma com seu dropped_iterations).
// TAXA = iterações/s no total (leitura = 0,9·TAXA, checkout = 0,1·TAXA; cada
// checkout faz 4 requisições). Também é o SOAK: DURACAO=30m.
import exec from 'k6/execution';
import { descobrir, leituraAleatoria, checkoutCompleto, thresholds, constante, TAXA, DURACAO } from './lib.js';

export const options = {
  scenarios: {
    leitura: constante('leitura', 9 * TAXA, '10s', DURACAO),
    checkout: constante('checkout', TAXA, '10s', DURACAO),
  },
  thresholds: thresholds({ checkout_fluxos_ok: ['count>0'] }),
};

export function setup() {
  return descobrir();
}

export function leitura(dados) {
  leituraAleatoria(dados);
}

export function checkout(dados) {
  checkoutCompleto(dados, dados.base + exec.scenario.iterationInTest);
}
