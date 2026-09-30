import { useState } from 'react'

import { mapaFake, ocupadosFake } from './fixture'
import { Legenda } from './Legenda'
import { MapaDeAssentos } from './MapaDeAssentos'

// Demonstração só em desenvolvimento (PRD 0019 RF06): o mapa com a fixture e
// estado local, para conferência visual antes da integração com a trava.
export function DemoMapa() {
  const [selecionados, setSelecionados] = useState<string[]>([])
  return (
    <section>
      <h1>Mapa de assentos (demonstração)</h1>
      <MapaDeAssentos
        mapa={mapaFake}
        ocupados={ocupadosFake}
        meus={['B9']}
        selecionados={selecionados}
        onAlternar={(codigo) => {
          setSelecionados((atual) => (atual.includes(codigo) ? atual.filter((c) => c !== codigo) : [...atual, codigo]))
        }}
      />
      <Legenda />
    </section>
  )
}
