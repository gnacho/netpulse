// probe.go: comprobación puntual del broker (#838) para el botón "probar
// conexión" de Ajustes.
package mqttpub

import (
	"context"
	"time"

	"github.com/gonzalop/mq"
)

// Probe hace un CONNECT contra el broker y desconecta.
func Probe(ctx context.Context, cfg Config) error {
	addr := brokerURL(cfg)
	opts := []mq.Option{
		mq.WithProtocolVersion(mq.ProtocolV311),
		mq.WithClientID("netpulse-probe"),
		mq.WithConnectTimeout(5 * time.Second),
	}
	if opt, ok := tlsOption(cfg); ok {
		opts = append(opts, opt)
	}
	if cfg.User != "" {
		opts = append(opts, mq.WithCredentials(cfg.User, cfg.Pass))
	}
	pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	client, err := mq.DialContext(pctx, addr, opts...)
	if err != nil {
		return err
	}
	client.Disconnect(pctx)
	return nil
}
