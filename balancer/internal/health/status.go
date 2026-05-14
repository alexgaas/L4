package health

import (
	"balancer/internal/balancer"
	"github.com/alexgaas/underdog"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/go-chi/chi/v5"
)

type StatusServerContext struct {
	LastSlbCheckout time.Time
	Checker         *balancer.Checker
}

func StartStatusServer(addr string, checker *balancer.Checker) {
	statusCtx := StatusServerContext{time.Time{}, checker}

	r := chi.NewRouter()
	r.Get("/debug/pprof/cmdline", pprof.Cmdline)
	r.Get("/debug/pprof/profile", pprof.Profile)
	r.Get("/debug/pprof/symbol", pprof.Symbol)
	r.Get("/debug/pprof/trace", pprof.Trace)
	r.Get("/debug/pprof/*", pprof.Index)

	r.Get("/backend_status", func(w http.ResponseWriter, r *http.Request) {
		handlerBackendStatus(w, r, &statusCtx)
	})
	r.Get("/last_slb_checkout", func(w http.ResponseWriter, r *http.Request) {
		handlerLast(w, r, &statusCtx)
	})
	r.Get("/ping", func(w http.ResponseWriter, r *http.Request) {
		handlerPing(w, r, &statusCtx)
	})
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		handlerDefault(w, r)
	})
	srv := &http.Server{
		Handler:      r,
		Addr:         addr,
		WriteTimeout: 31 * time.Second,
		ReadTimeout:  31 * time.Second,
	}
	balancer.Log.Info("Starting status server", log.Any("addr", addr))

	if err := srv.ListenAndServe(); err != nil {
		balancer.Log.Fatal("Unable to start status server", log.Error(err))
	}
}

func handlerDefault(w http.ResponseWriter, _ *http.Request) {
	if _, err := fmt.Fprintln(w, "no action was found"); err != nil {
		balancer.Log.Error("handlerDefault error", log.Error(err))
	}
}

func handlerPing(w http.ResponseWriter, _ *http.Request, c *StatusServerContext) {
	c.LastSlbCheckout = time.Now()
	if _, err := fmt.Fprintln(w, "ok"); err != nil {
		balancer.Log.Error("handlerPing error", log.Error(err))
	}
}

func handlerLast(w http.ResponseWriter, _ *http.Request, c *StatusServerContext) {
	if _, err := fmt.Fprintln(w, "last_slb_checkout", c.LastSlbCheckout); err != nil {
		balancer.Log.Error("handlerLast error", log.Error(err))
	}
}

func handlerBackendStatus(w http.ResponseWriter, _ *http.Request, c *StatusServerContext) {
	w.Header().Set("Content-Type", "text/plain")

	c.Checker.Status(w)
}
