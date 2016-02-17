package turms

import (
	"fmt"
	"golang.org/x/net/context"
	"sync"
)

type subscription struct {
	id         ID
	topic      URI
	sessionID  ID
	subscriber Conn
}

type broker struct {
	subscriptions map[ID]*subscription
	topics        map[URI][]*subscription

	mu sync.RWMutex
}

// Broker returns a handler that handles messages by routing incoming events
// from Publishers to Subscribers that are subscribed to respective topics.
// Requires a Realm handler chained before the Broker.
func Broker() Handler {
	return &broker{
		subscriptions: make(map[ID]*subscription),
		topics:        make(map[URI][]*subscription),
	}
}

func (b *broker) subscribe(topic URI, sessionID ID, idC *idCounter, conn Conn) ID {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscriptionID := idC.Next()
	sub := &subscription{
		id:         subscriptionID,
		topic:      topic,
		sessionID:  sessionID,
		subscriber: conn,
	}

	t, ok := b.topics[topic]
	if !ok {
		b.topics[topic] = []*subscription{sub}
		return subscriptionID
	}
	b.topics[topic] = append(t, sub)
	return subscriptionID
}

func (b *broker) unsubscribe(subscriptionID ID, sessionID ID) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub, ok := b.subscriptions[subscriptionID]
	if !ok {
		return &NoSuchSubscription
	}
	delete(b.subscriptions, subscriptionID)

	subs, ok := b.topics[sub.topic]
	if !ok || subs == nil {
		// This should not happen
		return nil
	}
	for i, s := range subs {
		if s.id == subscriptionID {
			// Delete element
			b.topics[sub.topic], subs[len(subs)-1] = append(subs[:i], subs[i+1:]...), nil
			return nil
		}
	}
	// This should not happen
	return nil
}

func (b *broker) getSubscriptions(topic URI) []*subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if subs, ok := b.topics[topic]; ok {
		return subs
	}
	return []*subscription{}
}

// Handle processes incoming Broker related messages and may communicated back with the connection in the provided context.
func (b *broker) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, fmt.Errorf("Broker requires a session stored in the context"))
	}

	switch m := msg.(type) {
	case *Subscribe:
		topicID := b.subscribe(m.Topic, se.ID, se.routerIDGen, conn)
		subscribedMsg := &Subscribed{SubscribedCode, m.Request, topicID}
		err := conn.Send(ctx, subscribedMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
	case *Unsubscribe:
		err := b.unsubscribe(m.Subscription, se.ID)
		if err != nil {
			errMsg := &Error{ErrorCode,
				UnsubscribeCode, m.Request, map[string]interface{}{}, "wamp.error.no_such_subscription", nil, nil,
			}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, fmt.Errorf("No such subscription"))
		}

		msg := &Unsubscribed{UnsubscribedCode, m.Request}
		err = conn.Send(ctx, msg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
	case *Publish:
		subscriptions := b.getSubscriptions(m.Topic)

		publicationID := NewGlobalID()
		for _, sub := range subscriptions {
			eventMsg := &Event{EventCode,
				sub.id, publicationID, map[string]interface{}{}, m.Args, m.ArgsKW,
			}
			if err := sub.subscriber.Send(ctx, eventMsg); err != nil {
				// TODO depending on error drop subscriber
			}
		}

		if acknowledgeVal, ok := m.Options["acknowledge"]; ok {
			if acknowledge, ok := acknowledgeVal.(bool); ok && acknowledge {
				acknowledgeMsg := &Published{PublishedCode, m.Request, publicationID}
				if err := conn.Send(ctx, acknowledgeMsg); err != nil {
					return NewErrorContext(ctx, err)
				}
			}
			return ctx
		}
	}
	return ctx
}
