package database

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/mkrebs03/atalaya/internal/crypto"
	"github.com/mkrebs03/atalaya/internal/notifications"
)

var _ notifications.Repository = (*Database)(nil)

func (database *Database) ListChannels(ctx context.Context, limit int, cursor *notifications.ChannelCursor, includeArchived bool) ([]notifications.Channel, error) {
	if limit < 1 {
		return nil, notifications.ErrInvalid
	}
	query := `SELECT id FROM notification_channels WHERE (? OR archived_at IS NULL)`
	args := []any{includeArchived}
	if cursor != nil {
		query += ` AND (lower(name)>lower(?) OR (lower(name)=lower(?) AND id>?))`
		args = append(args, cursor.Name, cursor.Name, cursor.ID)
	}
	query += ` ORDER BY lower(name),id LIMIT ?`
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifications.Channel, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := database.GetChannel(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) GetChannel(ctx context.Context, id string) (notifications.Channel, error) {
	return readChannel(ctx, database.sql, id)
}

func (database *Database) CreateChannel(ctx context.Context, channel notifications.Channel, secret string, now time.Time, random notifications.IDSource) (notifications.Channel, error) {
	if random == nil || channel.Provider != notifications.ProviderDiscord || notifications.ValidateChannelName(channel.Name) != nil || notifications.ValidateDiscordWebhook(secret) != nil {
		return notifications.Channel{}, notifications.ErrInvalid
	}
	id, err := random.ID()
	if err != nil {
		return notifications.Channel{}, err
	}
	secretEnvelope, err := crypto.Seal(database.key, alertSecretType, id, []byte(secret))
	if err != nil {
		return notifications.Channel{}, err
	}
	now = now.UTC()
	if _, err := database.sql.ExecContext(ctx, `INSERT INTO notification_channels(id,name,provider,encrypted_config,enabled,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,1)`, id, channel.Name, channel.Provider, secretEnvelope, boolInt(channel.Enabled), dbTime(now), dbTime(now)); err != nil {
		return notifications.Channel{}, err
	}
	return database.GetChannel(ctx, id)
}

func (database *Database) UpdateChannel(ctx context.Context, id string, version int64, channel notifications.Channel, secret *string, now time.Time) error {
	if channel.Provider != notifications.ProviderDiscord || notifications.ValidateChannelName(channel.Name) != nil {
		return notifications.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var currentVersion, enabled int
	var currentEnvelope []byte
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,enabled,encrypted_config,archived_at FROM notification_channels WHERE id=?", id).Scan(&currentVersion, &enabled, &currentEnvelope, &archived); errors.Is(err, sql.ErrNoRows) {
		return notifications.ErrNotFound
	} else if err != nil {
		return err
	}
	if currentVersion != int(version) {
		return notifications.ErrVersionConflict
	}
	if archived.Valid {
		return notifications.ErrArchived
	}
	envelope := currentEnvelope
	if secret != nil {
		if notifications.ValidateDiscordWebhook(*secret) != nil {
			return notifications.ErrInvalid
		}
		envelope, err = crypto.Seal(database.key, alertSecretType, id, []byte(*secret))
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE notification_channels SET name=?,provider=?,encrypted_config=?,enabled=?,updated_at=?,version=version+1 WHERE id=? AND version=?", channel.Name, channel.Provider, envelope, boolInt(channel.Enabled), dbTime(now), id, version); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) ArchiveChannel(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,archived_at FROM notification_channels WHERE id=?", id).Scan(&current, &archived); errors.Is(err, sql.ErrNoRows) {
		return notifications.ErrNotFound
	} else if err != nil {
		return err
	}
	if current != version {
		return notifications.ErrVersionConflict
	}
	if archived.Valid {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, "UPDATE notification_channels SET enabled=0,archived_at=?,updated_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), dbTime(now), id, version); err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) RestoreChannel(ctx context.Context, id string, version int64, now time.Time) error {
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current int64
	var archived sql.NullString
	if err := tx.QueryRowContext(ctx, "SELECT version,archived_at FROM notification_channels WHERE id=?", id).Scan(&current, &archived); errors.Is(err, sql.ErrNoRows) {
		return notifications.ErrNotFound
	} else if err != nil {
		return err
	}
	if current != version {
		return notifications.ErrVersionConflict
	}
	if !archived.Valid {
		return tx.Commit()
	}
	_, err = tx.ExecContext(ctx, "UPDATE notification_channels SET archived_at=NULL,enabled=0,updated_at=?,version=version+1 WHERE id=? AND version=?", dbTime(now), id, version)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (database *Database) EnqueueTest(ctx context.Context, channelID, requestID string, now time.Time, random notifications.IDSource) (notifications.Delivery, bool, error) {
	if random == nil || notifications.ValidateRequestID(requestID) != nil {
		return notifications.Delivery{}, false, notifications.ErrInvalid
	}
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return notifications.Delivery{}, false, err
	}
	defer tx.Rollback()
	var archived sql.NullString
	var channelName string
	if err := tx.QueryRowContext(ctx, "SELECT name,archived_at FROM notification_channels WHERE id=?", channelID).Scan(&channelName, &archived); errors.Is(err, sql.ErrNoRows) {
		return notifications.Delivery{}, false, notifications.ErrNotFound
	} else if err != nil {
		return notifications.Delivery{}, false, err
	}
	if archived.Valid {
		return notifications.Delivery{}, false, notifications.ErrArchived
	}
	id, err := random.ID()
	if err != nil {
		return notifications.Delivery{}, false, err
	}
	now = now.UTC()
	payload := notifications.TestPayload(channelName)
	result, err := tx.ExecContext(ctx, `INSERT INTO notification_deliveries(id,kind,channel_id,test_request_id,payload,state,attempts,next_attempt_at,created_at,updated_at) VALUES(?,'test',?,?,?,'pending',0,?,?,?) ON CONFLICT(channel_id,test_request_id) WHERE kind='test' DO NOTHING`, id, channelID, requestID, string(payload), dbTime(now), dbTime(now), dbTime(now))
	if err != nil {
		return notifications.Delivery{}, false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return notifications.Delivery{}, false, err
	}
	var deliveryID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM notification_deliveries WHERE kind='test' AND channel_id=? AND test_request_id=?", channelID, requestID).Scan(&deliveryID); err != nil {
		return notifications.Delivery{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return notifications.Delivery{}, false, err
	}
	if inserted == 0 {
		item, getErr := database.GetDelivery(ctx, deliveryID)
		return item, true, getErr
	}
	item, err := database.GetDelivery(ctx, deliveryID)
	return item, false, err
}

func (database *Database) ListDeliveries(ctx context.Context, channelID, state string, limit int, cursor *notifications.DeliveryCursor) ([]notifications.Delivery, error) {
	if limit < 1 {
		return nil, notifications.ErrInvalid
	}
	query := "SELECT id FROM notification_deliveries WHERE 1=1"
	args := make([]any, 0, 8)
	if channelID != "" {
		query += " AND channel_id=?"
		args = append(args, channelID)
	}
	if state != "" {
		query += " AND state=?"
		args = append(args, state)
	}
	if cursor != nil {
		query += " AND (created_at<? OR (created_at=? AND id<?))"
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	query += " ORDER BY created_at DESC,id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := database.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]notifications.Delivery, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		item, err := database.GetDelivery(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (database *Database) GetDelivery(ctx context.Context, id string) (notifications.Delivery, error) {
	var delivery notifications.Delivery
	var kind, state, next, created, updated string
	var incidentID, transition, requestID, lease, errorCode, skipped, delivered sql.NullString
	if err := database.sql.QueryRowContext(ctx, `SELECT id,kind,channel_id,incident_id,transition,test_request_id,state,attempts,next_attempt_at,lease_until,last_error_code,skipped_reason,created_at,updated_at,delivered_at FROM notification_deliveries WHERE id=?`, id).Scan(&delivery.ID, &kind, &delivery.ChannelID, &incidentID, &transition, &requestID, &state, &delivery.Attempts, &next, &lease, &errorCode, &skipped, &created, &updated, &delivered); errors.Is(err, sql.ErrNoRows) {
		return notifications.Delivery{}, notifications.ErrNotFound
	} else if err != nil {
		return notifications.Delivery{}, err
	}
	delivery.Kind = notifications.DeliveryKind(kind)
	delivery.State = notifications.DeliveryState(state)
	delivery.NextAttemptAt, _ = parseDBTime(next)
	delivery.CreatedAt, _ = parseDBTime(created)
	delivery.UpdatedAt, _ = parseDBTime(updated)
	if incidentID.Valid {
		delivery.IncidentID = &incidentID.String
	}
	if transition.Valid {
		value := notifications.Transition(transition.String)
		delivery.Transition = &value
	}
	if requestID.Valid {
		delivery.TestRequestID = &requestID.String
	}
	if lease.Valid {
		value, err := parseDBTime(lease.String)
		if err != nil {
			return notifications.Delivery{}, err
		}
		delivery.LeaseUntil = &value
	}
	if errorCode.Valid {
		delivery.LastErrorCode = errorCode.String
	}
	if skipped.Valid {
		delivery.SkippedReason = skipped.String
	}
	if delivered.Valid {
		value, err := parseDBTime(delivered.String)
		if err != nil {
			return notifications.Delivery{}, err
		}
		delivery.DeliveredAt = &value
	}
	return delivery, nil
}

func (database *Database) ClaimDelivery(ctx context.Context, now time.Time) (notifications.DeliveryClaim, error) {
	now = now.UTC()
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return notifications.DeliveryClaim{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='pending',lease_until=NULL,updated_at=? WHERE state='sending' AND lease_until<=?", dbTime(now), dbTime(now)); err != nil {
		return notifications.DeliveryClaim{}, err
	}
	for {
		var claim notifications.DeliveryClaim
		var kind, provider, payload string
		var encrypted []byte
		var attempts int
		var channelEnabled int
		var archived sql.NullString
		err = tx.QueryRowContext(ctx, `SELECT d.id,d.kind,d.channel_id,c.provider,c.encrypted_config,d.payload,d.attempts,c.enabled,c.archived_at FROM notification_deliveries d JOIN notification_channels c ON c.id=d.channel_id WHERE d.state='pending' AND d.next_attempt_at<=? AND (d.kind='test' OR d.transition='firing' OR NOT EXISTS (SELECT 1 FROM notification_deliveries prior WHERE prior.incident_id=d.incident_id AND prior.channel_id=d.channel_id AND prior.transition='firing' AND prior.state IN ('pending','sending'))) ORDER BY d.created_at,d.id LIMIT 1`, dbTime(now)).Scan(&claim.DeliveryID, &kind, &claim.ChannelID, &provider, &encrypted, &payload, &attempts, &channelEnabled, &archived)
		if errors.Is(err, sql.ErrNoRows) {
			if commitErr := tx.Commit(); commitErr != nil {
				return notifications.DeliveryClaim{}, commitErr
			}
			return notifications.DeliveryClaim{}, notifications.ErrNoDueDelivery
		}
		if err != nil {
			return notifications.DeliveryClaim{}, err
		}
		if archived.Valid || channelEnabled == 0 {
			reason := "channel_disabled"
			if archived.Valid {
				reason = "channel_archived"
			}
			if _, err := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='skipped',skipped_reason=?,updated_at=? WHERE id=? AND state='pending'", reason, dbTime(now), claim.DeliveryID); err != nil {
				return notifications.DeliveryClaim{}, err
			}
			continue
		}
		secret, err := crypto.Open(database.key, alertSecretType, claim.ChannelID, encrypted)
		if err != nil {
			if _, updateErr := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='failed',attempts=6,last_error_code='secret_unavailable',updated_at=? WHERE id=? AND state='pending'", dbTime(now), claim.DeliveryID); updateErr != nil {
				return notifications.DeliveryClaim{}, updateErr
			}
			continue
		}
		claim.Kind = notifications.DeliveryKind(kind)
		claim.Provider = notifications.Provider(provider)
		claim.WebhookURL = string(secret)
		claim.Payload = []byte(payload)
		claim.Attempt = attempts + 1
		claim.LeaseUntil = now.Add(notifications.DeliveryLease)
		if _, err := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='sending',attempts=attempts+1,lease_until=?,updated_at=? WHERE id=? AND state='pending' AND attempts=?", dbTime(claim.LeaseUntil), dbTime(now), claim.DeliveryID, attempts); err != nil {
			clear(secret)
			return notifications.DeliveryClaim{}, err
		}
		clear(secret)
		if err := tx.Commit(); err != nil {
			claim.Clear()
			return notifications.DeliveryClaim{}, err
		}
		return claim, nil
	}
}

func (database *Database) FinishDelivery(ctx context.Context, claim notifications.DeliveryClaim, outcome notifications.DeliveryOutcome, now time.Time, random notifications.IDSource) error {
	if random == nil {
		return notifications.ErrInvalid
	}
	now = now.UTC()
	tx, err := database.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var attempts int
	if err := tx.QueryRowContext(ctx, "SELECT attempts FROM notification_deliveries WHERE id=? AND state='sending' AND lease_until=?", claim.DeliveryID, dbTime(claim.LeaseUntil)).Scan(&attempts); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if outcome.Delivered {
		if _, err := tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='delivered',lease_until=NULL,delivered_at=?,updated_at=? WHERE id=? AND state='sending' AND attempts=?", dbTime(now), dbTime(now), claim.DeliveryID, attempts); err != nil {
			return err
		}
		if claim.Kind == notifications.KindTest {
			if _, err := tx.ExecContext(ctx, "UPDATE notification_channels SET last_test_at=?,updated_at=? WHERE id=?", dbTime(now), dbTime(now), claim.ChannelID); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	errorCode := sanitizeDeliveryCode(outcome.ErrorCode)
	if errorCode == "" {
		errorCode = "delivery_failed"
	}
	if outcome.Retryable && attempts < 6 {
		delay := notifications.RetryDelay(attempts)
		if outcome.RetryAfter > delay {
			delay = outcome.RetryAfter
		}
		if delay > notifications.MaxRetryAfter {
			delay = notifications.MaxRetryAfter
		}
		delay += time.Duration(random.Intn(5000)) * time.Millisecond
		if delay > notifications.MaxRetryAfter {
			delay = notifications.MaxRetryAfter
		}
		_, err = tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='pending',lease_until=NULL,next_attempt_at=?,last_error_code=?,updated_at=? WHERE id=? AND state='sending' AND attempts=?", dbTime(now.Add(delay)), errorCode, dbTime(now), claim.DeliveryID, attempts)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE notification_deliveries SET state='failed',lease_until=NULL,last_error_code=?,updated_at=? WHERE id=? AND state='sending' AND attempts=?", errorCode, dbTime(now), claim.DeliveryID, attempts)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func readChannel(ctx context.Context, query queryer, id string) (notifications.Channel, error) {
	var channel notifications.Channel
	var provider string
	var archived, lastTest, created, updated sql.NullString
	if err := query.QueryRowContext(ctx, "SELECT id,name,provider,enabled,archived_at,last_test_at,created_at,updated_at,version,encrypted_config FROM notification_channels WHERE id=?", id).Scan(&channel.ID, &channel.Name, &provider, &channel.Enabled, &archived, &lastTest, &created, &updated, &channel.Version, new([]byte)); errors.Is(err, sql.ErrNoRows) {
		return notifications.Channel{}, notifications.ErrNotFound
	} else if err != nil {
		return notifications.Channel{}, err
	}
	channel.Provider = notifications.Provider(provider)
	channel.SecretConfigured = true
	if archived.Valid {
		value, err := parseDBTime(archived.String)
		if err != nil {
			return notifications.Channel{}, err
		}
		channel.ArchivedAt = &value
	}
	if lastTest.Valid {
		value, err := parseDBTime(lastTest.String)
		if err != nil {
			return notifications.Channel{}, err
		}
		channel.LastTestAt = &value
	}
	var err error
	if channel.CreatedAt, err = parseDBTime(created.String); err != nil {
		return notifications.Channel{}, err
	}
	if channel.UpdatedAt, err = parseDBTime(updated.String); err != nil {
		return notifications.Channel{}, err
	}
	return channel, nil
}

func sanitizeDeliveryCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 64 {
		return "delivery_failed"
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '-' {
			return "delivery_failed"
		}
	}
	return value
}
