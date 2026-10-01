// Exibição no fuso do cinema (a API fala UTC — refinamento E3/E5). Nunca
// depende do fuso da máquina de quem acessa.
const FUSO = 'America/Sao_Paulo'
const LOCALE = 'pt-BR'

const fmtHora = new Intl.DateTimeFormat(LOCALE, { timeZone: FUSO, hour: '2-digit', minute: '2-digit' })
const fmtDia = new Intl.DateTimeFormat(LOCALE, { timeZone: FUSO, weekday: 'short', day: 'numeric', month: 'short' })
const fmtChave = new Intl.DateTimeFormat('en-CA', { timeZone: FUSO, year: 'numeric', month: '2-digit', day: '2-digit' })
const fmtPreco = new Intl.NumberFormat(LOCALE, { style: 'currency', currency: 'BRL' })

/** "20:30" no fuso do cinema. */
export function hora(iso: string): string {
  return fmtHora.format(new Date(iso))
}

/** "qua., 1 de out." no fuso do cinema. */
export function dia(iso: string): string {
  return fmtDia.format(new Date(iso))
}

/** "2099-10-01": agrupa sessões pelo dia local do cinema. */
export function chaveDia(iso: string): string {
  return fmtChave.format(new Date(iso))
}

/** "R$ 32,00" (o preço vem do servidor em centavos). */
export function preco(centavos: number): string {
  return fmtPreco.format(centavos / 100)
}

/** "2h 22min". */
export function duracao(minutos: number): string {
  const h = Math.floor(minutos / 60)
  const m = minutos % 60
  if (h === 0) {
    return `${String(m)}min`
  }
  return m === 0 ? `${String(h)}h` : `${String(h)}h ${String(m)}min`
}

/** "09:41" — tempo restante de um hold (nunca negativo). */
export function contagem(ms: number): string {
  const total = Math.max(0, Math.ceil(ms / 1000))
  const min = Math.floor(total / 60)
  const seg = total % 60
  return `${String(min).padStart(2, '0')}:${String(seg).padStart(2, '0')}`
}

/**
 * "2099-10-01T20:30" (datetime-local, no fuso do cinema) → ISO UTC. O Brasil
 * não tem horário de verão desde 2019: o fuso do cinema é UTC−3 fixo.
 */
export function paraUTCDoCinema(local: string): string {
  return new Date(`${local}:00-03:00`).toISOString()
}
