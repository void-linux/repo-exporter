package cache

import (
	"context"
	"regexp"
	"strings"

	"github.com/golang/groupcache"
)

// RepoDataStorage store and search and store data through groupcache
type RepoDataStorage struct {
	group *groupcache.Group
}

// NewRepoDataStorage build a new repodata storage
func NewRepoDataStorage(name string, cacheBytes int64, peers string, getter func(string) ([]byte, error)) RepoDataStorage {
	p := strings.Split(peers, ",")
	pool := groupcache.NewHTTPPool(p[0])
	pool.Set(p...)

	group := groupcache.NewGroup(name, cacheBytes, groupcache.GetterFunc(
		func(ctx groupcache.Context, key string, dst groupcache.Sink) error {
			var re = regexp.MustCompile(`{{\d*}}`)
			s := re.ReplaceAllString(key, "")
			b, err := getter(s)
			if err != nil {
				return err
			}
			dst.SetBytes(b)
			return nil
		},
	))
	return RepoDataStorage{
		group: group,
	}
}

// Get searches by repodata storage and store in dst
func (r RepoDataStorage) Get(ctx context.Context, key string, dst *[]byte) error {
	return r.group.Get(ctx, key, groupcache.AllocatingByteSliceSink(dst))
}
