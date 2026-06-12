package domain

// ArticuloAlmacen mirrors one row from the article-existence-by-almacen
// query: SALDOS_IN summed per articulo joined against ARTICULOS,
// LINEAS_ARTICULOS, and the configured PRECIOS_EMPRESA price lists. The
// Precios field is the legacy-shape concatenated string
// "<NOMBRE_LISTA>:<PRECIO>,<NOMBRE_LISTA>:<PRECIO>" produced by Firebird's
// LIST aggregate — the frontend already splits on this format so we
// preserve it verbatim.
type ArticuloAlmacen struct {
	ArticuloID      int
	Articulo        string
	Existencias     int64
	LineaArticuloID int
	LineaArticulo   string
	Precios         string
}
