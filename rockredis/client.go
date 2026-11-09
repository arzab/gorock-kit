package rockredis

import "github.com/redis/go-redis/v9"

// Client returns the underlying go-redis client of a Service created by
// NewService, for commands the Service interface does not cover:
//
//	rank, err := rockredis.Client(svc).ZRevRank(ctx, "leaderboard", "alice").Result()
//
// It returns nil before Init, after a failed Init, and for Service values not
// created by NewService (e.g. test doubles). It is a function rather than a
// Service method so that existing Service implementations keep compiling.
func Client(s Service) *redis.Client {
	impl, ok := s.(*service)
	if !ok {
		return nil
	}
	return impl.client
}
