package turms

import (
	"golang.org/x/net/context"
	"sync"
	"testing"
	"time"
)

type logHandler struct {
	logf func(format string, v ...interface{})
}

func (h *logHandler) Handle(ctx context.Context, c Conn, msg Message) context.Context {
	se, ok := ClientSessionFromContext(ctx)
	if !ok {
		h.logf("[DEBUG][Realm:n/a][Session:n/a]: %#v", msg)
		return ctx
	}
	h.logf("[DEBUG][Realm:%s][Session:%d]: %#v", se.Realm(), se.ID(), msg)
	return ctx
}

const timeout = time.Second

func newTimeoutContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), timeout)
	return ctx
}

func setupRouter(t *testing.T) *Router {
	// Setup router and middleware
	log := &logHandler{logf: t.Logf}
	router := NewRouter()
	rh := NewRealm()
	broker := NewBroker()
	dealer := NewDealer()
	if testing.Verbose() {
		router.Handler = &Chain{
			log,
			rh,
			broker,
			dealer,
		}
	} else {
		router.Handler = &Chain{
			rh,
			broker,
			dealer,
		}
	}

	// Setup realms
	realms := []string{
		"TestRouterBasicProfile", "TestRouterMultipleClients",
	}
	for _, name := range realms {
		err := rh.CreateRealm(name)
		if err != nil {
			t.Logf("%s", err)
		}
	}
	return router
}

func TestRouterBasicProfile(t *testing.T) {
	router := setupRouter(t)
	var err error
	// Subscribe & Publish
	realmName := URI("TestRouterBasicProfile")
	publisher := router.Client()
	err = publisher.JoinRealm(newTimeoutContext(), realmName, &ClientDetails{Publisher: true})
	if err != nil {
		t.Error(err)
	}
	subscriber := router.Client()
	err = subscriber.JoinRealm(newTimeoutContext(), realmName, &ClientDetails{Subscriber: true})
	if err != nil {
		t.Error(err)
	}

	sub, err := subscriber.Subscribe(newTimeoutContext(), "test")
	if err != nil {
		t.Error(err)
	}

	eventText := "Hello"
	err = publisher.Publish(newTimeoutContext(), &PublishOption{true}, "test", []interface{}{eventText}, nil)
	if err != nil {
		t.Error(err)
	}

	select {
	case <-time.After(timeout):
		t.Errorf("Did not receive an event in %s.", timeout)
	case event := <-sub:
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
	err = callee.JoinRealm(newTimeoutContext(), realmName, &ClientDetails{Callee: true})
	if err != nil {
		t.Error(err)
	}

	caller := router.Client()
	err = caller.JoinRealm(newTimeoutContext(), realmName, &ClientDetails{Caller: true})
	if err != nil {
		t.Error(err)
	}

	procedureName := URI("com.test")
	helloCallee := "Hello Callee"
	helloCaller := "Hello Caller"
	procedureArgs := []interface{}{helloCallee}
	procedureArgsKW := map[string]interface{}{"test": "value"}
	err = callee.Register(newTimeoutContext(), procedureName, func(ctx context.Context, inv *Invocation) ([]interface{}, map[string]interface{}, error) {
		arg := inv.Args[0].(string)
		if arg != helloCallee {
			t.Errorf("Expected args[0] to be %s got %s", helloCallee, arg)
		}
		kw, ok := inv.ArgsKW["test"]
		if !ok {
			t.Errorf("Expected a value at argsKW[\"test\"]")
		}
		testVal, ok := kw.(string)
		if !ok {
			t.Errorf("Expected argsKW[\"test\"] to be of type string got %T ", testVal)
		} else if testVal != "value" {
			t.Errorf("Expected argsKW[\"test\"] to be %s got %s", procedureArgsKW["test"], testVal)
		}
		return []interface{}{helloCaller}, nil, nil
	})
	if err != nil {
		t.Error(err)
	}

	res, err := caller.Call(newTimeoutContext(), procedureName, procedureArgs, procedureArgsKW)
	if err != nil {
		t.Error(err)
	}
	if res.Args[0].(string) != helloCaller {
		t.Errorf("Expected res.Args[0] to be %s got %s", helloCaller, res.Args[0].(string))
	}

	err = callee.Close()
	if err != nil {
		t.Error(err)
	}
	err = caller.Close()
	if err != nil {
		t.Error(err)
	}
	err = router.Close()
	if err != nil {
		t.Error(err)
	}
}

func TestRouterMultipleClients(t *testing.T) {
	router := setupRouter(t)

	numClients := 10
	clients := make([]*Client, numClients)
	for i := range clients {
		clients[i] = router.Client()
	}
	realmName := URI("TestRouterMultipleClients")
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *Client) {
			err := c.JoinRealm(newTimeoutContext(), realmName, &ClientDetails{true, true, true, true})
			if err != nil {
				t.Error(err)
			}
			wg.Done()
		}(client)
	}
	wg.Wait()

	// Close router
	err := router.Close()
	if err != nil {
		t.Error(err)
	}
}
