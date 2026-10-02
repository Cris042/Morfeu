// Checkout fim a fim (PRD 0045 RF09): trava → pedido → /__teste/pagar → pedido
// pago (ingressos emitidos no pivô; o invariante confere no banco). TAXA é de
// FLUXOS por segundo (cada um faz 4 requisições). A latência do fluxo é o SLI
// do checkout (checkout_fluxo_duration); o veredito oficial é server-side.
import exec from 'k6/execution';
import { descobrir, checkoutCompleto, thresholds, constante } from './lib.js';

export const options = {
  scenarios: { checkout: constante('checkout') },
  thresholds: thresholds({ checkout_fluxos_ok: ['count>0'] }),
};

export function setup() {
  return descobrir();
}

export function checkout(dados) {
  checkoutCompleto(dados, dados.base + exec.scenario.iterationInTest);
}
