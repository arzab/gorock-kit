package rockdelivery

// Route is what an App serves: a named Action plus transport metadata —
// options only a transport understands, such as an HTTP success status or a
// queue's retry policy. Business code never reads metadata.
//
//	var Status = rockdelivery.NewMetaKey[int]("http.status") // declared by the HTTP transport
//
//	route := rockdelivery.NewRoute("orders.create", rockdelivery.Bind(orders.Create),
//	    rockdelivery.WithMeta(Status, 201))
type Route struct {
	Action
	name string
	meta map[any]any
}

// RouteOption configures a Route.
type RouteOption func(*Route)

// NewRoute names an Action and attaches metadata. The name identifies the
// route in failures and observations (logs, metrics).
func NewRoute(name string, a Action, opts ...RouteOption) Route {
	if a.call == nil {
		panic("rockdelivery: NewRoute with an unbound Action (use Bind)")
	}
	r := Route{Action: a, name: name}
	for _, opt := range opts {
		opt(&r)
	}
	return r
}

// Name returns the route name.
func (r Route) Name() string { return r.name }

// MetaKey is a typed key for route metadata. Declare one per option, in the
// transport package; keys are compared by identity, so two keys with the same
// name never clash.
type MetaKey[T any] struct{ name string }

// NewMetaKey declares a metadata key. name is for debugging only.
func NewMetaKey[T any](name string) *MetaKey[T] { return &MetaKey[T]{name: name} }

// String returns the key's name.
func (k *MetaKey[T]) String() string { return k.name }

// WithMeta sets a metadata value on the route.
func WithMeta[T any](k *MetaKey[T], v T) RouteOption {
	return func(r *Route) {
		m := make(map[any]any, len(r.meta)+1) // copy: routes may share option slices
		for key, val := range r.meta {
			m[key] = val
		}
		m[k] = v
		r.meta = m
	}
}

// Meta reads a metadata value from the route.
func Meta[T any](r Route, k *MetaKey[T]) (T, bool) {
	v, ok := r.meta[k]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// MetaOr reads a metadata value, or returns def when it is not set.
func MetaOr[T any](r Route, k *MetaKey[T], def T) T {
	if v, ok := Meta(r, k); ok {
		return v
	}
	return def
}
