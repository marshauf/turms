package turms

import (
	"golang.org/x/net/context"
	"log"
	"sync"
	"testing"
)

const (
	realmName = "test"
)

type logHandler struct {
	logf func(format string, v ...interface{})
}

func (h *logHandler) Handle(ctx context.Context, c Conn, msg Message) context.Context {
	se, ok := SessionFromContext(ctx)
	if !ok {
		h.logf("[DEBUG][UnknownSession]: %#v", msg)
		return ctx
	}
	h.logf("[DEBUG][%d]: %#v", se.ID(), msg)
	return ctx
}

func TestRouterBasicProfile(t *testing.T) {
	router := NewRouter()

	verbose := true
	if verbose {
		router.Handler = &Chain{
			Realm(realmName),
			&logHandler{
				log.Printf,
			},
			&Chain{
				Broker(),
				Dealer(),
			},
		}
	} else {
		router.Handler = &Chain{
			Realm(realmName),
			&Chain{
				Broker(),
				Dealer(),
			},
		}
	}

	// Subscribe & Publish
	publisher := router.Client()
	err := publisher.JoinRealm(context.Background(), realmName, &ClientDetails{Publisher: true})
	if err != nil {
		t.Error(err)
	}

	subscriber := router.Client()
	err = subscriber.JoinRealm(context.Background(), realmName, &ClientDetails{Subscriber: true})
	if err != nil {
		t.Error(err)
	}

	sub, err := subscriber.Subscribe(context.Background(), "test")
	if err != nil {
		t.Error(err)
	}

	eventText := "Hello"
	err = publisher.Publish(context.Background(), &PublishOption{true}, "test", []interface{}{eventText}, nil)
	if err != nil {
		t.Error(err)
	}

	event := <-sub
	if len(event.Args) == 0 {
		t.Errorf("Received event %v should contain one argument", event)
	} else {
		text, ok := event.Args[0].(string)
		if !ok {
			t.Errorf("Expected argument 0 to be of type string, got %T", event.Args[0])
		}
		if text != eventText {
			t.Errorf("Expected argument 0 to be %s, got %s", eventText, text)
		}
	}

	err = publisher.Close()
	if err != nil {
		t.Error(err)
	}

	err = subscriber.Close()
	if err != nil {
		t.Error(err)
	}

	// Register & Call
	callee := router.Client()
	err = callee.JoinRealm(context.Background(), realmName, &ClientDetails{Callee: true})
	if err != nil {
		t.Error(err)
	}

	caller := router.Client()
	err = caller.JoinRealm(context.Background(), realmName, &ClientDetails{Caller: true})
	if err != nil {
		t.Error(err)
	}

	procedureName := URI("com.test")
	helloCallee := "Hello Callee"
	helloCaller := "Hello Caller"
	procedureArgs := []interface{}{helloCallee}
	procedureArgsKW := map[string]interface{}{"test": "value"}
	err = callee.Register(context.Background(), procedureName, func(ctx context.Context, inv *Invocation) ([]interface{}, map[string]interface{}, error) {
		arg := inv.Args[0].(string)
		if arg != helloCallee {
			t.Errorf("Expected args[0] to be %s got %s", helloCallee, arg)
		}
		kw := inv.ArgsKW["test"].(string)
		if kw != "value" {
			t.Errorf("Expected argsKW[\"test\"] to be %s got %s", "value", arg)
		}
		return []interface{}{helloCaller}, nil, nil
	})
	if err != nil {
		t.Error(err)
	}

	res, err := caller.Call(context.Background(), procedureName, procedureArgs, procedureArgsKW)
	if err != nil {
		t.Error(err)
	}
	if res.Args[0].(string) != helloCaller {
		t.Errorf("Expected res.Args[0] to be %s got %s", helloCaller, res.Args[0].(string))
	}

	err = router.Close()
	if err != nil {
		t.Error(err)
	}

	err = router.Close()
	if err == nil {
		t.Errorf("router.Close should return an error if its already closed")
	}
}

func TestRouterMultipleClients(t *testing.T) {
	router := NewRouter()
	defer func() {
		err := router.Close()
		if err != nil {
			t.Error(err)
		}
	}()
	router.Handler = &Chain{
		Realm(realmName),
		&Chain{
			Broker(),
			Dealer(),
		},
	}

	numClients := 10
	clients := make([]*Client, numClients)
	for i := range clients {
		clients[i] = router.Client()
	}

	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			err := c.JoinRealm(context.Background(), realmName, &ClientDetails{true, true, true, true})
			if err != nil {
				t.Error(err)
			}
			wg.Done()
		}(client)
	}
	wg.Wait()

	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			err := c.Close()
			if err != nil {
				t.Error(err)
			}
			wg.Done()
		}(client)
	}
	wg.Wait()
}
