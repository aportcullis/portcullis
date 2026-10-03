package pgdialect_test

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/aportcullis/portcullis/internal/domain/connection"
	"github.com/aportcullis/portcullis/internal/domain/query"
	"github.com/aportcullis/portcullis/internal/infra/pgdialect"
	"github.com/jackc/pgx/v5/pgproto3"
)

func TestExecutionRejectsMissingOrMismatchedStringReportBeforeSQL(t *testing.T) {
	for _, reported := range []string{"", "off"} {
		t.Run("report="+reported, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			observed := make(chan error, 1)
			go func() { observed <- observeStringReportRejection(listener, reported) }()
			host, portText, err := net.SplitHostPort(listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				t.Fatal(err)
			}
			target, err := connection.NewTarget(host, port, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			credential, err := connection.NewCredential("fixture", "synthetic")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			stream, err := pgdialect.New(pgdialect.Options{}).Execute(ctx, target, connection.TLSModeDisable, credential, query.Execution{SQL: "SELECT 1", Class: query.ClassRead, Governed: true, MaxRows: 10, MaxResultBytes: 4096, TimeoutSeconds: 30})
			if stream != nil {
				_ = stream.Close()
			}
			var rejection *query.Rejection
			if !errors.As(err, &rejection) {
				t.Errorf("untrusted string report did not fail closed: %T", err)
			}
			select {
			case err := <-observed:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("server observation did not finish")
			}
		})
	}
}

func observeStringReportRejection(listener net.Listener, reported string) error {
	conn, err := listener.Accept()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	backend := pgproto3.NewBackend(conn, conn)
	startup, err := backend.ReceiveStartupMessage()
	if err != nil {
		return err
	}
	parameters, ok := startup.(*pgproto3.StartupMessage)
	if !ok || parameters.Parameters["standard_conforming_strings"] != "on" {
		return errors.New("client did not pin string interpretation")
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "server_version", Value: "18.6"})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	if reported != "" {
		backend.Send(&pgproto3.ParameterStatus{Name: "standard_conforming_strings", Value: reported})
	}
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return err
	}
	message, err := backend.Receive()
	if err != nil {
		return err
	}
	if _, closed := message.(*pgproto3.Terminate); !closed {
		return errors.New("client sent SQL before validating the server report")
	}
	return nil
}
