package train

import (
	"encoding/json"
	"time"

	"github.com/bytearena/core/common/mq"
)

type MemoryMessageClient struct {
	subscriptions *mq.SubscriptionMap
}

func NewMemoryMessageClient() (*MemoryMessageClient, error) {
	c := &MemoryMessageClient{
		subscriptions: mq.NewSubscriptionMap(),
	}

	return c, nil
}

func (client *MemoryMessageClient) Subscribe(channel string, topic string, onmessage mq.SubscriptionCallback) error {
	client.subscriptions.Set(channel+":"+topic, onmessage)
	return nil
}

func (client *MemoryMessageClient) Publish(channel string, topic string, payload interface{}) error {

	subscription := client.subscriptions.Get(channel + ":" + topic)
	if subscription != nil {
		res, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		go subscription(mq.BrokerMessage{
			Timestamp: time.Now().Format(time.RFC3339),
			Topic:     topic,
			Channel:   channel,
			Data:      res,
		})
	}

	return nil
}
