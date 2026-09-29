package identidade

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// parametrosRapidos deixam os testes de unidade rápidos; o formato e a
// leitura dos parâmetros do hash são os mesmos da produção.
var parametrosRapidos = ParametrosArgon2{MemoriaKiB: 8 * 1024, Iteracoes: 1, Paralelismo: 1}

// TestHash_FormatoPHCEVerificacao cobre CA09.
func TestHash_FormatoPHCEVerificacao(t *testing.T) {
	h1, err := gerarHash("senha-correta-123", parametrosRapidos)
	if err != nil {
		t.Fatalf("gerarHash: %v", err)
	}
	if !strings.HasPrefix(h1, "$argon2id$v=19$m=8192,t=1,p=1$") || strings.Count(h1, "$") != 5 {
		t.Errorf("hash fora do formato PHC: %s", h1)
	}
	h2, _ := gerarHash("senha-correta-123", parametrosRapidos)
	if h1 == h2 {
		t.Error("salts distintos deveriam gerar hashes distintos para a mesma senha")
	}

	ok, err := verificarHash("senha-correta-123", h1)
	if err != nil || !ok {
		t.Fatalf("senha correta rejeitada: ok=%v err=%v", ok, err)
	}
	if ok, _ := verificarHash("senha-errada-123", h1); ok {
		t.Error("senha errada aceita")
	}

	// Parâmetros vêm do próprio hash: um hash com custo padrão verifica mesmo
	// que o chamador não saiba os parâmetros (rehash futuro sem migração).
	hPadrao, _ := gerarHash("outra-senha-123", ParametrosArgon2Padrao)
	if !strings.Contains(hPadrao, "m=19456,t=2,p=1") {
		t.Errorf("parâmetros padrão (OWASP) ausentes: %s", hPadrao)
	}
	if ok, err := verificarHash("outra-senha-123", hPadrao); err != nil || !ok {
		t.Errorf("hash padrão não verificou: %v", err)
	}
}

func TestHash_Malformado(t *testing.T) {
	casos := []string{
		"", "texto", "$argon2i$v=19$m=8192,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=18$m=8192,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=0,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=8192,t=1,p=1$!!!$aGFzaA",
		"$argon2id$v=19$m=8192,t=1,p=1$c2FsdA$",
	}
	for _, c := range casos {
		if _, err := verificarHash("x", c); !errors.Is(err, errHashMalformado) {
			t.Errorf("%q deveria ser malformado, veio %v", c, err)
		}
	}
}

func TestSenhaAleatoria(t *testing.T) {
	vistas := map[string]bool{}
	for i := 0; i < 50; i++ {
		s, err := gerarSenhaAleatoria()
		if err != nil {
			t.Fatalf("gerarSenhaAleatoria: %v", err)
		}
		if utf8.RuneCountInString(s) != 24 || vistas[s] {
			t.Fatalf("senha inválida ou repetida: %q", s)
		}
		vistas[s] = true
	}
}

// TestValidarRegistro cobre a parte de validação/normalização do CA01.
func TestValidarRegistro(t *testing.T) {
	nome, email, err := validarRegistro(EntradaRegistro{Nome: "  Ana  ", Email: "  Ana@Exemplo.COM ", Senha: "12345678"})
	if err != nil || nome != "Ana" || email != "ana@exemplo.com" {
		t.Fatalf("normalização falhou: %q %q %v", nome, email, err)
	}

	casos := map[string]struct {
		in    EntradaRegistro
		campo string
	}{
		"nome vazio":      {EntradaRegistro{Nome: "  ", Email: "a@b.co", Senha: "12345678"}, "nome"},
		"nome 121":        {EntradaRegistro{Nome: strings.Repeat("a", 121), Email: "a@b.co", Senha: "12345678"}, "nome"},
		"email sem @":     {EntradaRegistro{Nome: "A", Email: "ana.exemplo.com", Senha: "12345678"}, "email"},
		"email com nome":  {EntradaRegistro{Nome: "A", Email: "Ana <ana@b.co>", Senha: "12345678"}, "email"},
		"senha 7":         {EntradaRegistro{Nome: "A", Email: "a@b.co", Senha: "1234567"}, "senha"},
		"senha 129 runas": {EntradaRegistro{Nome: "A", Email: "a@b.co", Senha: strings.Repeat("é", 129)}, "senha"},
	}
	for nomeCaso, c := range casos {
		_, _, err := validarRegistro(c.in)
		var ev *ErroValidacao
		if !errors.As(err, &ev) || len(ev.Campos) != 1 || ev.Campos[0] != c.campo {
			t.Errorf("%s: esperava campo %q, veio %v", nomeCaso, c.campo, err)
		}
		if !errors.Is(err, ErrDadosInvalidos) {
			t.Errorf("%s: deveria embrulhar ErrDadosInvalidos", nomeCaso)
		}
	}
}
