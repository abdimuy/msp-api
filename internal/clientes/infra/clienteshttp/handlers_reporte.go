//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Reporte handles GET /clientes/{id}/reporte.
//
// It assembles the client's full report read-model, renders it as a PDF via
// clientespdf.Render, and streams the bytes with appropriate HTTP headers.
// Auth is inherited from the chi middleware already applied to the router by
// the caller — no additional auth check is performed here.
func (h *Handlers) Reporte(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	// Parse client ID from the URL path.
	idStr := chi.URLParam(req, "id")
	clienteID, err := strconv.Atoi(idStr)
	if err != nil || clienteID <= 0 {
		http.Error(w, "id de cliente inválido", http.StatusBadRequest)
		return
	}

	// Parse the optional repeated ?venta=<id> query parameters.
	ventaStrs := req.URL.Query()["venta"]
	ventaIDs := make([]int, 0, len(ventaStrs))
	for _, vs := range ventaStrs {
		id, err := strconv.Atoi(vs)
		if err != nil || id <= 0 {
			http.Error(w, "parámetro venta inválido: "+vs, http.StatusBadRequest)
			return
		}
		ventaIDs = append(ventaIDs, id)
	}

	// Assemble the report read-model from the repository.
	rep, err := h.svc.GenerarReporteCliente(ctx, clienteID, ventaIDs)
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) && ae.Kind == apperror.KindNotFound {
			http.Error(w, "cliente no encontrado", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "error al generar el reporte pdf del cliente",
			"cliente_id", clienteID,
			"error", err.Error(),
		)
		http.Error(w, "error interno al generar el reporte", http.StatusInternalServerError)
		return
	}

	// Render PDF bytes from the assembled read-model.
	pdfBytes, err := clientespdf.Render(rep, time.Now())
	if err != nil {
		slog.ErrorContext(ctx, "error al renderizar el pdf del reporte",
			"cliente_id", clienteID,
			"error", err.Error(),
		)
		http.Error(w, "error interno al renderizar el reporte", http.StatusInternalServerError)
		return
	}

	// Stream the PDF to the client with the appropriate headers.
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf(`inline; filename="reporte-cliente-%d.pdf"`, clienteID))
	w.Header().Set("Content-Length", strconv.Itoa(len(pdfBytes)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pdfBytes)
}
