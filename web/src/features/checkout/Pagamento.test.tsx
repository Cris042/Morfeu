import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { apiFalsa } from '../../test/renderizar'

const confirmPayment = vi.fn()

vi.mock('@stripe/stripe-js', () => ({ loadStripe: vi.fn(() => Promise.resolve({})) }))
vi.mock('@stripe/react-stripe-js', () => ({
  Elements: ({ children }: { children: React.ReactNode }) => children,
  PaymentElement: () => <div>campos do cartão</div>,
  useStripe: () => ({ confirmPayment }),
  useElements: () => ({ id: 'elements' }),
}))

afterEach(() => {
  vi.unstubAllEnvs()
  vi.unstubAllGlobals()
  confirmPayment.mockReset()
})

async function pagamentoCom(env: Record<string, string>) {
  vi.resetModules()
  for (const [k, v] of Object.entries(env)) {
    vi.stubEnv(k, v)
  }
  return (await import('./Pagamento')).Pagamento
}

describe('meio de pagamento', () => {
  it('modo fake: "Pagar (teste)" chama a rota de teste e conclui', async () => {
    const f = apiFalsa({ '/api/__teste/pagar/p-1': { status: 204 } })
    const Pagamento = await pagamentoCom({ VITE_PAGAMENTO_MODO: 'fake' })
    const aoConcluir = vi.fn()
    render(<Pagamento pedidoId="p-1" segredo="s" aoConcluir={aoConcluir} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Pagar (teste)' }))
    await vi.waitFor(() => {
      expect(aoConcluir).toHaveBeenCalledOnce()
    })
    const init = f.mock.calls[0]?.[1]
    expect(init?.method).toBe('POST')
    expect((init?.headers as Record<string, string>)['X-Requested-With']).toBe('morfeu')
    expect(screen.queryByText('campos do cartão')).not.toBeInTheDocument()
  })

  it('modo fake: falha da rota mostra erro e permite tentar de novo', async () => {
    apiFalsa({ '/api/__teste/pagar/p-1': { status: 500 } })
    const Pagamento = await pagamentoCom({ VITE_PAGAMENTO_MODO: 'fake' })
    render(<Pagamento pedidoId="p-1" segredo="s" aoConcluir={vi.fn()} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Pagar (teste)' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('O pagamento de teste falhou.')
    expect(screen.getByRole('button', { name: 'Pagar (teste)' })).toBeEnabled()
  })

  it('Stripe: confirma sem redirecionamento; recusa mostra a mensagem e a 2ª tentativa conclui', async () => {
    const Pagamento = await pagamentoCom({ VITE_STRIPE_PK: 'pk_test_x' })
    confirmPayment
      .mockResolvedValueOnce({ error: { message: 'Seu cartão foi recusado.' } })
      .mockResolvedValueOnce({ paymentIntent: { status: 'succeeded' } })
    const aoConcluir = vi.fn()
    render(<Pagamento pedidoId="p-1" segredo="pi_x_secret_y" aoConcluir={aoConcluir} />)
    expect(await screen.findByText('campos do cartão')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Pagar (teste)' })).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Pagar' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Seu cartão foi recusado.')
    expect(aoConcluir).not.toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Pagar' }))
    await vi.waitFor(() => {
      expect(aoConcluir).toHaveBeenCalledOnce()
    })
    expect(confirmPayment).toHaveBeenCalledWith({ elements: { id: 'elements' }, redirect: 'if_required' })
  })
})
