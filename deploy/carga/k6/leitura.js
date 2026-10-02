// Leitura pública: cartaz, sessões do filme e ocupação (PRD 0045 RF09).
//
// PERFIL=constante (padrão)  taxa fixa TAXA req/s por DURACAO — baseline (quente/frio)
//                            e patamar do SLO (ex.: TAXA=300 DURACAO=10m).
// PERFIL=rampa               ramping-arrival-rate de RAMPA_DE a RAMPA_ATE req/s em
//                            PASSOS degraus lineares de PASSO cada (ex.: 50→400).
//
// Frio = esvaziar o cache antes (docs/carga/runbook.md). discardResponseBodies:
// o corpo não é usado — só status e a latência medida no servidor.
import { descobrir, leituraAleatoria, thresholds, constante, TAXA, VUS_PRE, VUS_MAX } from './lib.js';

const PERFIL = __ENV.PERFIL || 'constante';
const PASSO = __ENV.PASSO || '4s';
const PASSOS = Number(__ENV.PASSOS || 5);
const RAMPA_DE = Number(__ENV.RAMPA_DE || 50);
const RAMPA_ATE = Number(__ENV.RAMPA_ATE || 400);

function rampa() {
  const stages = [];
  for (let i = 1; i <= PASSOS; i++) {
    stages.push({ target: Math.round(RAMPA_DE + ((RAMPA_ATE - RAMPA_DE) * i) / PASSOS), duration: PASSO });
  }
  return {
    executor: 'ramping-arrival-rate',
    exec: 'leitura',
    startRate: RAMPA_DE,
    timeUnit: '1s',
    stages,
    preAllocatedVUs: VUS_PRE,
    maxVUs: VUS_MAX,
  };
}

export const options = {
  discardResponseBodies: true,
  scenarios: { leitura: PERFIL === 'rampa' ? rampa() : constante('leitura', TAXA) },
  thresholds: thresholds(),
};

export function setup() {
  return descobrir();
}

export function leitura(dados) {
  leituraAleatoria(dados);
}
