package storage

// Layout es la lista canónica de raíces de almacenamiento de una instancia:
// cada ruta ya resuelta (absoluta, con symlinks resueltos, creada si no
// existía) una sola vez al arrancar. Formaliza el requisito de que ningún
// módulo calcule rutas de almacenamiento a mano (actualización 2026-09-10,
// §6): quien necesite una raíz de almacenamiento la pide aquí.
//
// Deliberadamente NO cubre la conexión a base de datos ni el destino de los
// logs: el DSN de la BD y logging.output son cadenas de configuración
// explícitas por diseño (un motor SQL necesita una ruta real; un logger a
// través de una abstracción de almacenamiento asíncrona sería frágil). Esas
// rutas siguen viniendo de config.Config directamente.
type Layout struct {
	// UserData es la raíz del contenido de archivos de usuario del pool por
	// defecto. Los pools adicionales (Fase D) resuelven su propia raíz de la
	// misma forma vía ResolveRoot.
	UserData string
	// Thumbnails, Versions y Temp quedan resueltas y creadas ya, aunque sus
	// funcionalidades todavía no las consuman: así reubicar cualquiera de
	// ellas a otro disco (config.Storage.*Dir) funciona desde el primer día
	// sin tocar este código.
	Thumbnails string
	Versions   string
	Temp       string
}

// LayoutParams son las rutas tal como las devuelve config.Config (ya con
// los overrides por área aplicados, pero sin resolver todavía). server.go
// hace de puente: internal/storage no importa internal/config, para
// mantener el paquete de almacenamiento libre de configuración como hasta
// ahora.
type LayoutParams struct {
	UserData   string
	Thumbnails string
	Versions   string
	Temp       string
}

// NewLayout resuelve cada raíz a su forma canónica (absoluta + symlinks
// resueltos) y la crea si no existe. Falla si alguna no se puede resolver
// o crear: es un error de arranque, no algo que diferir.
func NewLayout(p LayoutParams) (*Layout, error) {
	userData, err := ResolveRoot(p.UserData)
	if err != nil {
		return nil, err
	}
	thumbnails, err := ResolveRoot(p.Thumbnails)
	if err != nil {
		return nil, err
	}
	versions, err := ResolveRoot(p.Versions)
	if err != nil {
		return nil, err
	}
	temp, err := ResolveRoot(p.Temp)
	if err != nil {
		return nil, err
	}
	return &Layout{
		UserData:   userData,
		Thumbnails: thumbnails,
		Versions:   versions,
		Temp:       temp,
	}, nil
}
