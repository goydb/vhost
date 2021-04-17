package vhost

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/goydb/goydb/pkg/goydb"
	"github.com/goydb/goydb/pkg/model"
	"github.com/goydb/goydb/pkg/port"
	"github.com/goydb/goydb/pkg/zipvfs"

	"github.com/gorilla/mux"
	"github.com/mitchellh/mapstructure"
)

const (
	VirtualHostDB     = "_admin"
	VirtualHostPrefix = "goydb.vhost:"
	VirtualHostFiles  = "files.zip"
)

// Middleware to handle virtual hosts
func Middleware(gdb goydb.Goydb) http.Handler {
	vh := VirtualHost{
		Storage: gdb,
	}
	go vh.Run(context.Background())
	mdw := vh.Middleware()(gdb.Handler)
	return mdw
}

// VirtualHost
// Virtual host implements a http middleware. The middleware is
// configured using the vhost configration in the _admin database.
type VirtualHost struct {
	Storage port.Storage

	vhosts []*model.VirtualHostConfiguration
	lookup map[string]http.Handler

	next http.Handler
}

func (c *VirtualHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, ok := c.lookup[host(r.Host)]
	if ok {
		handler.ServeHTTP(w, r)
		return
	}
	c.next.ServeHTTP(w, r)
}

// FindAllVHosts find all virtual host configurations in the database
func (c *VirtualHost) FindAllVHosts(ctx context.Context) ([]*model.VirtualHostConfiguration, error) {
	var config []*model.VirtualHostConfiguration

	db, err := c.Storage.Database(ctx, VirtualHostDB)
	if err != nil { // not having the db means no config
		return nil, nil
	}

	docs, _, err := db.AllDocs(ctx, port.AllDocsQuery{
		StartKey:    VirtualHostPrefix,
		EndKey:      VirtualHostPrefix + "\uFFFF",
		IncludeDocs: true,
	})
	if err != nil {
		return nil, err
	}

	for _, doc := range docs {
		vhost := new(model.VirtualHostConfiguration)
		err := mapstructure.Decode(doc.Data, vhost)
		if err != nil {
			log.Print("error loading vhost %q: %v", doc.ID, err)
			continue
		}

		if vhost.Static != "" {
			att, err := db.GetAttachment(ctx, doc.ID, vhost.Static)
			if err != nil {
				log.Print("error get atatchment files for vhost %q: %v", doc.ID, err)
				continue
			}
			defer att.Reader.Close()

			fs, err := zipvfs.BuildFileSystem(ctx, att.Reader)
			if err != nil {
				log.Print("error building FS for vhost %q: %v", doc.ID, err)
				continue
			}

			vhost.FS = fs
		}

		config = append(config, vhost)
	}

	return config, nil
}

func (c *VirtualHost) Middleware() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		c.next = next
		return c
	}
}

func (c *VirtualHost) Run(ctx context.Context) {
	// todo update on change or after some time
	err := c.RebuildHandler()
	if err != nil {
		log.Printf("Error creating vhosts handler: %v", err)
	}
}

func (c *VirtualHost) RebuildHandler() error {
	config, err := c.FindAllVHosts(context.Background())
	if err != nil {
		return fmt.Errorf("failed creating vhosts: %v", err)
	}
	log.Printf("%v", config)

	c.vhosts = config
	c.lookup = make(map[string]http.Handler)
	for _, vhost := range config {
		for _, domain := range vhost.Domains {
			m := mux.NewRouter()

			for prefix, proxy := range vhost.Proxy {
				var handler http.Handler
				switch proxy.Type {
				case model.ProxyDB:
					handler = NewProxyDBHandler(prefix, proxy.Target, c)
				case model.ProxyReverse:
					u, err := url.Parse(proxy.Target)
					if err != nil {
						log.Printf("Error creating proxy for %q: %v", domain, err)
						continue
					}

					rp := httputil.NewSingleHostReverseProxy(u)
					origDirector := rp.Director
					rp.Director = func(r *http.Request) {
						origDirector(r)
						r.Host = u.Host
					}

					if proxy.StripPrefix {
						handler = http.StripPrefix(prefix, rp)
					} else {
						handler = rp
					}
				default:
					log.Printf("Error creating proxy for %q type: %v", domain, vhost)
					continue
				}

				m.PathPrefix(prefix).Handler(handler)
			}

			m.PathPrefix("/").Handler(http.FileServer(vhost.FS))

			c.lookup[domain] = m
		}
	}

	return nil
}

func NewProxyDBHandler(prefix, targetDB string, c *VirtualHost) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = new(url.URL)
		*r2.URL = *r.URL
		r2.URL.Path = "/" + targetDB + strings.TrimPrefix(r.URL.Path, prefix)
		c.next.ServeHTTP(w, r2)
	})
}

// FIXME host check
func host(host string) string {
	i := strings.Index(host, ":")
	if i > -1 {
		return host[:i]
	}
	return host
}
