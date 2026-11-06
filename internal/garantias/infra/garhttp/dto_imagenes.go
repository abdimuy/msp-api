package garhttp

import "github.com/danielgtaylor/huma/v2"

// ImagenDTO es la representación pública de una imagen.
// La huella nunca se expone por HTTP.
type ImagenDTO struct {
	ID          string `json:"id" format:"uuid"`
	Tipo        string `json:"tipo"`
	Description string `json:"descripcion"` //nolint:misspell // nombre JSON definido por el contrato en español.
	SubidaPor   string `json:"subida_por"`
	CreatedAt   string `json:"created_at" format:"date-time"`
	URL         string `json:"url"`
}

// ObtenerImagenRequest identifica la imagen dentro de garantía y evento.
type ObtenerImagenRequest struct {
	ID       string `path:"id"`
	EventoID string `path:"eid"`
	ImagenID string `path:"iid"`
}

// ObtenerImagenResponse devuelve el blob tal cual fue almacenado.
type ObtenerImagenResponse struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

// AdjuntarImagenBody es el formulario multipart para subir evidencia.
type AdjuntarImagenBody struct {
	Archivo     huma.FormFile `form:"archivo" required:"false" doc:"Imagen JPEG o PNG, máximo 10 MB"`
	Description string        `form:"descripcion" required:"false" maxLength:"500"` //nolint:misspell // contrato HTTP en español
}

// AdjuntarImagenRequest identifica el evento y contiene la evidencia.
type AdjuntarImagenRequest struct {
	EncabezadoIdempotencia
	ID       string `path:"id" format:"uuid"`
	EventoID string `path:"eid" format:"uuid"`
	RawBody  huma.MultipartFormFiles[AdjuntarImagenBody]
}

// AdjuntarImagenResponse devuelve la metadata pública de la evidencia.
type AdjuntarImagenResponse struct {
	Body ImagenDTO
}
