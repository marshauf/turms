package turms

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

type JSONCodec struct{}

func (jc *JSONCodec) Encode(w io.Writer, msg Message) error {
	_, err := w.Write([]byte(fmt.Sprintf("[%d", msg.Code())))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	numFields := v.NumField()
	until := numFields
	for i := numFields; i > 0; i-- {
		field := v.Field(i - 1)
		if field.Kind() != reflect.Map || field.Kind() != reflect.Slice {
			break
		}
		if field.IsNil() {
			until = i
		} else {
			break
		}
	}
	for i := 0; i < until; i++ {
		_, err = w.Write([]byte(","))
		if err != nil {
			return err
		}
		err = enc.Encode(v.Field(i).Interface())
		if err != nil {
			return err
		}
	}
	_, err = w.Write([]byte("]"))
	return err
}

func (jc *JSONCodec) Decode(r io.Reader) (Message, error) {
	raw := []json.RawMessage{}
	dec := json.NewDecoder(r)
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	var code Code
	if err := json.Unmarshal(raw[0], &code); err != nil {
		return nil, err
	}
	msgTyp, exist := messages[code]
	if !exist {
		return nil, fmt.Errorf("Unknown message code %d", code)
	}
	vPtr := reflect.New(msgTyp)
	v := vPtr.Elem()

	for i := 1; i < len(raw); i++ {
		field := v.Field(i - 1)
		if field.Kind() != reflect.Ptr {
			field = field.Addr()
		}
		if err := json.Unmarshal(raw[i], field.Interface()); err != nil {
			return nil, err
		}
	}
	return vPtr.Interface().(Message), nil
}
