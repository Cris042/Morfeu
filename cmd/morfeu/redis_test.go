package main

import (
	"strings"
	"testing"
)

// TestNovoRedis cobre RF04: URL com senha e o formato legado host:porta.
func TestNovoRedis(t *testing.T) {
	c, err := novoRedis("redis://:s3nha@redis:6379/2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if o := c.Options(); o.Addr != "redis:6379" || o.Password != "s3nha" || o.DB != 2 {
		t.Errorf("opções inesperadas: addr=%q db=%d (senha ok=%t)", o.Addr, o.DB, o.Password == "s3nha")
	}

	legado, err := novoRedis("redis:6379")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = legado.Close() }()
	if o := legado.Options(); o.Addr != "redis:6379" || o.Password != "" {
		t.Errorf("formato legado: addr=%q", o.Addr)
	}
}

// TestNovoRedis_URLInvalidaNaoVazaSenha: o erro não ecoa a URL.
func TestNovoRedis_URLInvalidaNaoVazaSenha(t *testing.T) {
	_, err := novoRedis("redis://:s3nha@redis:porta-ruim/x")
	if err == nil {
		t.Fatal("URL inválida deveria falhar")
	}
	if strings.Contains(err.Error(), "s3nha") {
		t.Errorf("o erro vaza a senha: %v", err)
	}
}
