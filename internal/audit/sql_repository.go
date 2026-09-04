package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/porrii/nexuscloud/internal/db"
)

type SQLRepository struct {
	conn *db.Conn
}

func NewSQLRepository(conn *db.Conn) *SQLRepository {
	return &SQLRepository{conn: conn}
}

func (r *SQLRepository) RecordEvent(ctx context.Context, e *Event) error {
	var metaJSON any
	if len(e.Metadata) > 0 {
		b, err := json.Marshal(e.Metadata)
		if err != nil {
			return fmt.Errorf("serializando metadata de auditoría: %w", err)
		}
		metaJSON = string(b)
	}

	_, err := r.conn.ExecContext(ctx, `
		INSERT INTO audit_events (id, occurred_at, actor_user_id, event_type, target_type, target_id, ip, metadata_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, db.TimeToString(e.OccurredAt), nullStr(e.ActorUserID), e.EventType,
		nullStr(e.TargetType), nullStr(e.TargetID), nullStr(e.IP), metaJSON,
	)
	if err != nil {
		return fmt.Errorf("registrando evento de auditoría: %w", err)
	}
	return nil
}

func (r *SQLRepository) ListEvents(ctx context.Context, limit, offset int) ([]*Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.conn.QueryContext(ctx, `
		SELECT id, occurred_at, actor_user_id, event_type, target_type, target_id, ip, metadata_json
		FROM audit_events ORDER BY occurred_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("listando eventos de auditoría: %w", err)
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		var (
			e                                                   Event
			occurredAt                                          string
			actorUserID, targetType, targetID, ip, metadataJSON sql.NullString
		)
		if err := rows.Scan(&e.ID, &occurredAt, &actorUserID, &e.EventType, &targetType, &targetID, &ip, &metadataJSON); err != nil {
			return nil, err
		}
		if e.OccurredAt, err = db.StringToTime(occurredAt); err != nil {
			return nil, fmt.Errorf("parseando occurred_at: %w", err)
		}
		e.ActorUserID = actorUserID.String
		e.TargetType = targetType.String
		e.TargetID = targetID.String
		e.IP = ip.String
		if metadataJSON.Valid && metadataJSON.String != "" {
			if err := json.Unmarshal([]byte(metadataJSON.String), &e.Metadata); err != nil {
				return nil, fmt.Errorf("deserializando metadata de auditoría: %w", err)
			}
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
