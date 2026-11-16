package garhttp

// ObtenerCatalogosRequest representa la solicitud de consulta de catálogos.
// ObtenerCatalogosRequest representa la solicitud de consulta de catalogos.
type ObtenerCatalogosRequest struct{}

// CatalogosTransicionDTO representa una transición del catálogo de garantías.
// CatalogosTransicionDTO describe una transition del catalogo de garantias.
type CatalogosTransicionDTO struct {
	Origen     string   `json:"origen"`
	Etapa      string   `json:"etapa"`
	Siguientes []string `json:"siguientes"`
}

// ObtenerCatalogosResponse contiene la respuesta con los catálogos de garantías.
// ObtenerCatalogosResponse contiene los catalogos de garantias.
type ObtenerCatalogosResponse struct {
	Body struct {
		Etapas       []string                 `json:"etapas"`
		Ubicaciones  []string                 `json:"ubicaciones"`
		Estados      []string                 `json:"estados"`
		TiposEvento  []string                 `json:"tipos_evento"`
		Transiciones []CatalogosTransicionDTO `json:"transiciones"`
	}
}
