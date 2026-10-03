package execution

import "github.com/aportcullis/portcullis/internal/domain/connection"

type postgresDialectResolver struct{ dialect Dialect }

func (r postgresDialectResolver) ExecutionDialect(engine connection.DBType) (Dialect, error) {
	if engine != connection.DBTypePostgreSQL {
		return nil, connection.ErrUnsupportedDBType
	}
	return r.dialect, nil
}
