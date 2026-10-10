//go:build !(js && wasm)

package browser

import "context"

func Fetch(ctx context.Context, _ FetchRequest) (FetchResponse, error) {
	if err := fetchContext(ctx); err != nil {
		return FetchResponse{}, err
	}
	return FetchResponse{}, ErrFetchUnavailable
}
