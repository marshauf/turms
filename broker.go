package turms

import (
	"errors"
	"golang.org/x/net/context"
	"sync"
)

var (
	ErrNoSession = errors.New("session does not exist in context")
	ErrNoSub     = errors.New("subscription does not exist")
)

type Subscription struct {
	id         ID
	topic      URI
	sessionID  ID
	subscriber Conn
}

type broker struct {
	subscriptions map[ID]*Subscription
	topics        map[URI][]*Subscription

	mu sync.RWMutex
}

type Broker interface {
	Subscribe(ctx context.Context, topic URI, se *Session, conn Conn) *Subscription
	Unsubscribe(ctx context.Context, subscription ID, session ID) error
	Publish(ctx context.Context, topic URI, details map[string]interface{}, args []interface{}, argsKW map[string]interface{}) error
	Subscriptions(ctx context.Context, topic URI) []*Subscription
}

// NewBroker returns a handler that handles messages by routing incoming events
// from Publishers to Subscribers that are subscribed to respective topics.
// Requires a Realm handler chained before the Broker.
func NewBroker() *broker {
	return &broker{
		subscriptions: make(map[ID]*Subscription),
		topics:        make(map[URI][]*Subscription),
	}
}

func (b *broker) Subscribe(ctx context.Context, topic URI, se *Session, conn Conn) *Subscription {
	return b.subscribe(topic, se.ID(), se.routerIDGen, conn)
}

func (b *broker) Unsubscribe(ctx context.Context, subscription ID, session ID) error {
	return b.unsubscribe(subscription, session)
}

func (b *broker) Publish(ctx context.Context, topic URI, details map[string]interface{}, args []interface{}, argsKW map[string]interface{}) error {
	subs := b.Subscriptions(ctx, topic)
	publicationID := NewGlobalID()
	for _, sub := range subs {
		event := &Event{EventCode,
			sub.id, publicationID, details, args, argsKW,
		}
		err := sub.subscriber.Send(ctx, event)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *broker) Subscriptions(ctx context.Context, topic URI) []*Subscription {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if subs, ok := b.topics[topic]; ok {
		return subs
	}
	return []*Subscription{}
}

func (b *broker) subscribe(topic URI, sessionID ID, idC *idCounter, conn Conn) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	subscriptionID := idC.Next()
	sub := &Subscription{
		id:         subscriptionID,
		topic:      topic,
		sessionID:  sessionID,
		subscriber: conn,
	}

	t, ok := b.topics[topic]
	if !ok {
		b.topics[topic] = []*Subscription{sub}
		return sub
	}
	b.topics[topic] = append(t, sub)
	b.subscriptions[subscriptionID] = sub
	return sub
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

// Handle processes incoming Broker related messages and may communicated back with the connection in the provided context.
func (b *broker) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, ErrNoSession)
	}

	switch m := msg.(type) {
	case *Subscribe:
		sub := b.subscribe(m.Topic, se.ID(), se.routerIDGen, conn)
		subscribedMsg := &Subscribed{SubscribedCode, m.Request, sub.id}
		err := conn.Send(ctx, subscribedMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
	case *Unsubscribe:
		err := b.unsubscribe(m.Subscription, se.ID())
		if err != nil {
			errMsg := &Error{ErrorCode,
				UnsubscribeCode, m.Request, map[string]interface{}{}, "wamp.error.no_such_subscription", nil, nil,
			}
			err = conn.Send(ctx, errMsg)
			if err != nil {
				return NewErrorContext(ctx, err)
			}
			return NewErrorContext(ctx, ErrNoSub)
		}

		msg := &Unsubscribed{UnsubscribedCode, m.Request}
		err = conn.Send(ctx, msg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
	case *Publish:
		subscriptions := b.Subscriptions(ctx, m.Topic)

		publicationID := NewGlobalID()
		for _, sub := range subscriptions {
			// Don't send a published event to the pubisher itself
			if se.ID() == sub.sessionID {
				continue
			}
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
