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

type broker struct{}

// Broker returns a handler that handles messages by routing incoming events
// from Publishers to Subscribers that are subscribed to respective topics.
// Requires a Realm handler chained before the Broker.
func Broker() *broker {
	return &broker{}
}

type brokerStore struct {
	subscriptions map[ID]*Subscription
	topics        map[URI][]*Subscription
	mu            sync.RWMutex
}

const (
	brokerStoreKey = "broker"
)

func getBrokerStore(r *Realm) *brokerStore {
	var store *brokerStore
	v, ok := r.Value(brokerStoreKey)
	if !ok {
		store = &brokerStore{
			subscriptions: make(map[ID]*Subscription),
			topics:        make(map[URI][]*Subscription),
		}
		r.SetValue(brokerStoreKey, store)
	} else {
		store = v.(*brokerStore)
	}
	return store
}

func subscribe(se *Session, conn Conn, req *Subscribe) *Subscription {
	r := se.Realm()
	store := getBrokerStore(r)
	sub := &Subscription{
		id:         se.RouterID(),
		topic:      req.Topic,
		sessionID:  se.ID(),
		subscriber: conn,
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	t, ok := store.topics[sub.topic]
	if !ok {
		store.topics[sub.topic] = []*Subscription{sub}
		return sub
	}
	store.topics[sub.topic] = append(t, sub)
	store.subscriptions[sub.id] = sub
	return sub
}

func unsubscribe(se *Session, subID ID) error {
	r := se.Realm()
	store := getBrokerStore(r)
	store.mu.Lock()
	defer store.mu.Unlock()
	sub, ok := store.subscriptions[subID]
	if !ok {
		return &NoSuchSubscription
	}
	delete(store.subscriptions, subID)
	subs := store.topics[sub.topic]
	for i, s := range subs {
		if s.id == subID {
			// Delete element
			store.topics[sub.topic], subs[len(subs)-1] = append(subs[:i], subs[i+1:]...), nil
			return nil
		}
	}
	panic("unreachable")
}

func publish(ctx context.Context, se *Session, req *Publish) ID {
	publicationID := NewGlobalID()
	r := se.Realm()
	store := getBrokerStore(r)
	store.mu.Lock()
	defer store.mu.Unlock()
	subs, ok := store.topics[req.Topic]
	if !ok {
		return publicationID
	}
	publisherID := se.ID()
	wg := sync.WaitGroup{}
	for _, sub := range subs {
		// Don't send a published event to the pubisher itself
		if publisherID == sub.sessionID {
			continue
		}
		wg.Add(1)
		go func(subscriber Conn, event *Event) {
			subscriber.Send(ctx, event)
			wg.Done()
		}(sub.subscriber, &Event{EventCode, sub.id, publicationID, map[string]interface{}{}, req.Args, req.ArgsKW})
	}
	wg.Wait()
	return publicationID
}

// Handle processes incoming Broker related messages and may communicated back with the connection in the provided context.
func (b *broker) Handle(ctx context.Context, conn Conn, msg Message) context.Context {
	se, hasSession := SessionFromContext(ctx)
	if !hasSession {
		return NewErrorContext(ctx, ErrNoSession)
	}

	switch m := msg.(type) {
	case *Subscribe:
		sub := subscribe(se, conn, m)
		subscribedMsg := &Subscribed{SubscribedCode, m.Request, sub.id}
		err := conn.Send(ctx, subscribedMsg)
		if err != nil {
			return NewErrorContext(ctx, err)
		}
		return ctx
	case *Unsubscribe:
		err := unsubscribe(se, m.Subscription)
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
		publicationID := publish(ctx, se, m)
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
