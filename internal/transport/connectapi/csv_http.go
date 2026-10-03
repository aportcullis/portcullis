package connectapi

import (
	"net/http"
	"strings"
	"time"
)

// BoundCSVWrites prevents a stalled CSV consumer from retaining a processing worker indefinitely.
func BoundCSVWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/ExportCSV") {
			next.ServeHTTP(writer, request)
			return
		}
		bounded := &csvResponseWriter{ResponseWriter: writer, controller: http.NewResponseController(writer)}
		_ = bounded.controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		next.ServeHTTP(bounded, request)
	})
}

type csvResponseWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
}

func (w *csvResponseWriter) Write(data []byte) (int, error) {
	if err := w.controller.SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return 0, err
	}
	return w.ResponseWriter.Write(data)
}

func (w *csvResponseWriter) Flush()                      { _ = w.controller.Flush() }
func (w *csvResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
