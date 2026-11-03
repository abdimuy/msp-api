package garhttp

// ImagenDTO es la representación pública de una imagen.
// La huella nunca se expone por HTTP.
type ImagenDTO struct {
	ID          string `json:"id" format:"uuid"`
	Tipo        string `json:"tipo"`
	Description string `json:"descripcion"`
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
