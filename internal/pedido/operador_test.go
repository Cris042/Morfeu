//go:build integration
// +build integration

package pedido

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mclovin137/morfeu/internal/autenticacao"
)

// Backoffice do pedido: consulta e cancelamento pelo operador (PRD 0037).

func (a *ambiente) tokenOperador(t *testing.T, id uuid.UUID) string {
	t.Helper()
	tok, _, err := a.emissor.Emitir(id, autenticacao.PapelOperador)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type detalheDTO struct {
	Pedido struct {
		ID            uuid.UUID `json:"id"`
		Email         string    `json:"email"`
		Status        Status    `json:"status"`
		MotivoEstorno string    `json:"motivo_estorno"`
	} `json:"pedido"`
	Ingressos []struct{ Assento, Status string } `json:"ingressos"`
	Eventos   []struct{ De, Para string }        `json:"eventos"`
}

func detalheDe(t *testing.T, r resposta) detalheDTO {
	t.Helper()
	var d detalheDTO
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &d) != nil {
		t.Fatalf("detalhe: %d %s", r.code, r.corpo)
	}
	return d
}

// TestOperador_Consulta: filtros, e-mail mascarado, nunca o código.
func TestOperador_Consulta(t *testing.T) {
	a := novoAmbiente(t)
	op := a.tokenOperador(t, uuid.New())
	p, sessaoID := a.pago(t)
	pendente, _, _ := a.pedidoCom(t, "C3")

	r := a.reqAuth(http.MethodGet, fmt.Sprintf("/backoffice/pedidos?sessao_id=%d&status=pago", sessaoID), "", op, nil)
	var lista struct {
		Pedidos []struct {
			ID     uuid.UUID `json:"id"`
			Email  string    `json:"email"`
			Status Status    `json:"status"`
		} `json:"pedidos"`
	}
	if r.code != http.StatusOK || json.Unmarshal([]byte(r.corpo), &lista) != nil || len(lista.Pedidos) != 1 ||
		lista.Pedidos[0].ID != p.ID || lista.Pedidos[0].Email != "A***@Exemplo.com" {
		t.Fatalf("lista: %d %s", r.code, r.corpo)
	}
	if strings.Contains(r.corpo, p.Codigo) || strings.Contains(r.corpo, "Ana@") || strings.Contains(r.corpo, `"codigo"`) {
		t.Fatalf("lista vaza código ou e-mail: %s", r.corpo)
	}
	if r := a.reqAuth(http.MethodGet, "/backoffice/pedidos?status=aguardando_pagamento", "", op, nil); !strings.Contains(r.corpo, pendente.ID.String()) || strings.Contains(r.corpo, p.ID.String()) {
		t.Fatalf("filtro por status: %s", r.corpo)
	}
	for _, q := range []string{"?status=inventado", "?sessao_id=abc", "?sessao_id=-1"} {
		if r := a.reqAuth(http.MethodGet, "/backoffice/pedidos"+q, "", op, nil); r.code != http.StatusBadRequest {
			t.Errorf("%s: %d", q, r.code)
		}
	}

	d := detalheDe(t, a.reqAuth(http.MethodGet, "/backoffice/pedidos/"+p.ID.String(), "", op, nil))
	if d.Pedido.Email != "A***@Exemplo.com" || len(d.Ingressos) != 2 || len(d.Eventos) != 2 || d.Eventos[1].Para != string(Pago) {
		t.Fatalf("detalhe: %+v", d)
	}
	if r := a.reqAuth(http.MethodGet, "/backoffice/pedidos/"+uuid.NewString(), "", op, nil); r.code != http.StatusNotFound {
		t.Fatalf("inexistente: %d", r.code)
	}
}

// TestOperador_Cancelar: sem janela, auditado na mesma TX com o ator do token.
func TestOperador_Cancelar(t *testing.T) {
	a := novoAmbiente(t)
	opID := uuid.New()
	op := a.tokenOperador(t, opID)
	p, _ := a.pago(t)
	// Dentro das 2h finais: o cliente não pode mais, o operador pode.
	a.irPara(inicioSessaoTeste.Add(-time.Hour))

	d := detalheDe(t, a.reqAuth(http.MethodPost, "/backoffice/pedidos/"+p.ID.String()+"/cancelar", "", op, nil))
	if d.Pedido.Status != EstornoPendente || d.Pedido.MotivoEstorno != MotivoOperador || d.Ingressos[0].Status != "cancelado" {
		t.Fatalf("cancelado: %+v", d)
	}
	var ator uuid.UUID
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT ator_id, count(*) OVER () FROM eventos_auditoria WHERE acao = 'pedido_cancelado' AND alvo_id = $1`,
		p.ID.String()).Scan(&ator, &n); err != nil || ator != opID || n != 1 {
		t.Fatalf("trilha: ator=%v n=%d err=%v", ator, n, err)
	}
	// Repetir: idempotente, sem segunda linha na trilha nem na métrica.
	detalheDe(t, a.reqAuth(http.MethodPost, "/backoffice/pedidos/"+p.ID.String()+"/cancelar", "", op, nil))
	if contarNoBanco(t, `SELECT count(*) FROM eventos_auditoria WHERE alvo_id = $1`, p.ID.String()) != 1 || lerContador(a.cancel, OrigemOperador) != 1 {
		t.Fatal("cancelamento repetido não pode auditar nem contar de novo")
	}

	// Sessão já começou → 409; pendente → 409; cliente → 403.
	outro, _ := a.pago(t)
	a.irPara(inicioSessaoTeste.Add(time.Minute))
	if r := a.reqAuth(http.MethodPost, "/backoffice/pedidos/"+outro.ID.String()+"/cancelar", "", op, nil); r.code != http.StatusConflict || r.corpo != `{"erro":"sessao_iniciada"}` {
		t.Fatalf("sessão iniciada: %d %s", r.code, r.corpo)
	}
	if r := a.reqAuth(http.MethodPost, "/backoffice/pedidos/"+outro.ID.String()+"/cancelar", "", a.tokenDe(t, uuid.New()), nil); r.code != http.StatusForbidden {
		t.Fatalf("cliente: %d", r.code)
	}
	if r := a.reqAuth(http.MethodGet, "/backoffice/pedidos", "", "", nil); r.code != http.StatusUnauthorized {
		t.Fatalf("sem token: %d", r.code)
	}
}
