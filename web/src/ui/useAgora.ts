import { useEffect, useState } from 'react'

/** Relógio de 1 s para contagens; só roda enquanto `ativo` (PRD 0020 RF06). */
export function useAgora(ativo: boolean): number {
  const [agora, setAgora] = useState(() => Date.now())
  useEffect(() => {
    if (!ativo) {
      return undefined
    }
    const id = setInterval(() => {
      setAgora(Date.now())
    }, 1000)
    return () => {
      clearInterval(id)
    }
  }, [ativo])
  return agora
}
