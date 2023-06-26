package harvest

import (
	"fmt"
	"net/url"
)

type requestOption func(v *url.Values)

func WithClientID(id int64) requestOption {
	return func(v *url.Values) {
		v.Set("client_id", fmt.Sprintf("%d", id))
	}
}
