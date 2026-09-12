package kafkax

import (
	"fmt"

	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	"github.com/twmb/franz-go/pkg/sasl/plain"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

// Cluster is how to reach the brokers: where they are, how to authenticate to
// them, and how many copies of each partition to ask for.
//
// One value rather than a broker list, because a list is all a single-broker
// laptop needs and a real cluster needs more — TLS, credentials, and a
// replication factor above one. Carrying them together means no client in any
// service can be built with the address but without the credentials.
type Cluster struct {
	Brokers []string

	// Replication is the replication factor EnsureTopics asks for. One on a
	// single-broker development cluster; three anywhere a broker can be lost
	// without losing data.
	Replication int16

	TLS  bool
	SASL sasl.Mechanism
}

// ClusterFromEnv reads the cluster from KAFKA_* variables.
//
// Unset means the local compose broker: plaintext, unauthenticated, one
// replica. A mechanism named without credentials, or one this does not know, is
// an error rather than an unauthenticated client that fails on first use.
func ClusterFromEnv() (Cluster, error) {
	cluster := Cluster{
		Brokers: config.Strings("KAFKA_BROKERS", []string{"localhost:19092"}),
	}

	replication, err := config.IntOr("KAFKA_REPLICATION_FACTOR", 1)
	if err != nil {
		return Cluster{}, err
	}
	if replication < 1 || replication > 32767 {
		return Cluster{}, &config.InvalidError{
			Key: "KAFKA_REPLICATION_FACTOR", Value: fmt.Sprint(replication),
			Want: "replication factor", Err: fmt.Errorf("must be between 1 and 32767"),
		}
	}
	cluster.Replication = int16(replication)

	if cluster.TLS, err = config.BoolOr("KAFKA_TLS", false); err != nil {
		return Cluster{}, err
	}

	mechanism := config.StringOr("KAFKA_SASL_MECHANISM", "")
	if mechanism == "" {
		return cluster, nil
	}

	user, err := config.String("KAFKA_SASL_USERNAME")
	if err != nil {
		return Cluster{}, err
	}
	pass, err := config.String("KAFKA_SASL_PASSWORD")
	if err != nil {
		return Cluster{}, err
	}

	switch mechanism {
	case "PLAIN":
		cluster.SASL = plain.Auth{User: user, Pass: pass}.AsMechanism()
	case "SCRAM-SHA-256":
		cluster.SASL = scram.Auth{User: user, Pass: pass}.AsSha256Mechanism()
	case "SCRAM-SHA-512":
		cluster.SASL = scram.Auth{User: user, Pass: pass}.AsSha512Mechanism()
	default:
		return Cluster{}, &config.InvalidError{
			Key: "KAFKA_SASL_MECHANISM", Value: mechanism, Want: "SASL mechanism",
			Err: fmt.Errorf("want PLAIN, SCRAM-SHA-256 or SCRAM-SHA-512"),
		}
	}

	return cluster, nil
}

// Options are the client options every connection to this cluster starts from:
// the seed brokers and, when configured, TLS and SASL.
func (c Cluster) Options() []kgo.Opt {
	options := []kgo.Opt{kgo.SeedBrokers(c.Brokers...)}
	if c.TLS {
		options = append(options, kgo.DialTLS())
	}
	if c.SASL != nil {
		options = append(options, kgo.SASL(c.SASL))
	}
	return options
}

// Client builds a client on this cluster with the given options on top.
func (c Cluster) Client(options ...kgo.Opt) (*kgo.Client, error) {
	return kgo.NewClient(append(c.Options(), options...)...)
}
