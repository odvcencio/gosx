//go:build !js || !wasm

package hubclient

type Client struct{}

func Dial(string, Options, Handler) (*Client, error) { return nil, ErrUnavailable }
func (*Client) Reconnect(string) error               { return ErrUnavailable }
func (*Client) Send(string, any) error               { return ErrUnavailable }
func (*Client) ReadyState() State                    { return Closed }
func (*Client) Close(int, string)                    {}
func (*Client) Dispose()                             {}
