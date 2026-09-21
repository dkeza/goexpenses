package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

type fakeApplicationServer struct {
	startError       error
	shutdownError    error
	started          chan struct{}
	stopped          chan struct{}
	shutdownCalled   chan struct{}
	stopOnce         sync.Once
	shutdownCallOnce sync.Once
	address          string
}

func newFakeApplicationServer() *fakeApplicationServer {
	return &fakeApplicationServer{
		started:        make(chan struct{}),
		stopped:        make(chan struct{}),
		shutdownCalled: make(chan struct{}),
	}
}

func (s *fakeApplicationServer) Start(address string) error {
	s.address = address
	close(s.started)
	if s.startError != nil {
		return s.startError
	}
	<-s.stopped
	return http.ErrServerClosed
}

func (s *fakeApplicationServer) Shutdown(context.Context) error {
	s.shutdownCallOnce.Do(func() { close(s.shutdownCalled) })
	s.stopOnce.Do(func() { close(s.stopped) })
	return s.shutdownError
}

func TestEmbeddedPostgresSchemaIsCurrentAndNonDestructive(t *testing.T) {
	schema, err := embeddedFiles.ReadFile("db/pg_structure.sql")
	if err != nil {
		t.Fatalf("read embedded PostgreSQL schema: %v", err)
	}
	schemaText := string(schema)

	if strings.Contains(strings.ToUpper(schemaText), "DROP TABLE") {
		t.Fatal("initial PostgreSQL schema contains a destructive DROP TABLE statement")
	}
	for _, required := range []string{
		"CREATE TABLE public.params",
		"created_at timestamp NOT NULL DEFAULT NOW()",
		"created_ts timestamp NOT NULL DEFAULT NOW()",
		"users_username_lower_uidx",
		"posts_active_account_date_id_idx",
		"posts_account_fk",
	} {
		if !strings.Contains(schemaText, required) {
			t.Fatalf("initial PostgreSQL schema does not contain %q", required)
		}
	}
}

func TestTemplatesParse(t *testing.T) {
	if _, err := parseTemplates(); err != nil {
		t.Fatalf("parse embedded templates: %v", err)
	}
}

func TestServeUntilShutdownStopsServerAfterContextCancellation(t *testing.T) {
	server := newFakeApplicationServer()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serveUntilShutdown(ctx, server, ":8080")
	}()

	<-server.started
	cancel()

	if err := <-result; err != nil {
		t.Fatalf("serveUntilShutdown: %v", err)
	}
	select {
	case <-server.shutdownCalled:
	default:
		t.Fatal("server shutdown was not called")
	}
	if server.address != ":8080" {
		t.Fatalf("server address = %q, want %q", server.address, ":8080")
	}
}

func TestServeUntilShutdownReturnsServerError(t *testing.T) {
	expectedError := errors.New("listen failed")
	server := newFakeApplicationServer()
	server.startError = expectedError

	err := serveUntilShutdown(context.Background(), server, ":8080")
	if !errors.Is(err, expectedError) {
		t.Fatalf("serveUntilShutdown error = %v, want %v", err, expectedError)
	}
	select {
	case <-server.shutdownCalled:
		t.Fatal("shutdown called after server startup failure")
	default:
	}
}

func TestServeUntilShutdownReturnsShutdownError(t *testing.T) {
	expectedError := errors.New("shutdown timed out")
	server := newFakeApplicationServer()
	server.shutdownError = expectedError
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serveUntilShutdown(ctx, server, ":8080")
	}()

	<-server.started
	cancel()

	if err := <-result; !errors.Is(err, expectedError) {
		t.Fatalf("serveUntilShutdown error = %v, want %v", err, expectedError)
	}
}
