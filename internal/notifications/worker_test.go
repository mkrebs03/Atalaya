package notifications

import (
	"context"
	"errors"
	"testing"
	"time"
)

type workerRepository struct {
	claim   DeliveryClaim
	claimed bool
	outcome DeliveryOutcome
	cancel  context.CancelFunc
}

func (*workerRepository) ListChannels(context.Context, int, *ChannelCursor, bool) ([]Channel, error) {
	return nil, nil
}
func (*workerRepository) GetChannel(context.Context, string) (Channel, error) { return Channel{}, nil }
func (*workerRepository) CreateChannel(context.Context, Channel, string, time.Time, IDSource) (Channel, error) {
	return Channel{}, nil
}
func (*workerRepository) UpdateChannel(context.Context, string, int64, Channel, *string, time.Time) error {
	return nil
}
func (*workerRepository) ArchiveChannel(context.Context, string, int64, time.Time) error { return nil }
func (*workerRepository) RestoreChannel(context.Context, string, int64, time.Time) error { return nil }
func (*workerRepository) EnqueueTest(context.Context, string, string, time.Time, IDSource) (Delivery, bool, error) {
	return Delivery{}, false, nil
}
func (*workerRepository) ListDeliveries(context.Context, string, string, int, *DeliveryCursor) ([]Delivery, error) {
	return nil, nil
}
func (*workerRepository) GetDelivery(context.Context, string) (Delivery, error) {
	return Delivery{}, nil
}

func (repository *workerRepository) ClaimDelivery(ctx context.Context, _ time.Time) (DeliveryClaim, error) {
	if repository.claimed {
		return DeliveryClaim{}, ctx.Err()
	}
	repository.claimed = true
	return repository.claim, nil
}

func (repository *workerRepository) FinishDelivery(_ context.Context, _ DeliveryClaim, outcome DeliveryOutcome, _ time.Time, _ IDSource) error {
	repository.outcome = outcome
	repository.cancel()
	return nil
}

type workerSender struct{}

func (workerSender) Send(context.Context, DeliveryClaim) SendResult {
	return SendResult{ErrorCode: "invalid_webhook", Err: ErrInvalidDiscordWebhook}
}

type workerClock struct{}

func (workerClock) Now() time.Time { return time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC) }

type workerRandom struct{}

func (workerRandom) ID() (string, error) { return "delivery-id", nil }
func (workerRandom) Intn(int) int        { return 0 }

func TestWorkerMarksInvalidWebhookPermanent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	repository := &workerRepository{
		claim:  DeliveryClaim{DeliveryID: "delivery-id", WebhookURL: "https://discord.com/api/webhooks/id/token"},
		cancel: cancel,
	}
	worker := Worker{Repository: repository, Sender: workerSender{}, Clock: workerClock{}, Random: workerRandom{}, PollInterval: time.Millisecond}
	if err := worker.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if repository.outcome.Retryable || repository.outcome.ErrorCode != "invalid_webhook" {
		t.Fatalf("outcome=%+v", repository.outcome)
	}
}
