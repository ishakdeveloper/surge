package kafkax_test

import (
	"errors"
	"testing"

	"github.com/ishakdeveloper/surge/shared/config"
	"github.com/ishakdeveloper/surge/shared/kafkax"
)

func clearKafkaEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"KAFKA_BROKERS", "KAFKA_REPLICATION_FACTOR", "KAFKA_TLS",
		"KAFKA_SASL_MECHANISM", "KAFKA_SASL_USERNAME", "KAFKA_SASL_PASSWORD",
	} {
		t.Setenv(key, "")
	}
}

// Unset is the compose broker, so `make dev-*` needs no Kafka configuration.
func TestClusterFromEnvDefaultsToTheLocalBroker(t *testing.T) {
	clearKafkaEnv(t)

	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cluster.Brokers) != 1 || cluster.Brokers[0] != "localhost:19092" {
		t.Errorf("brokers = %v, want the compose broker", cluster.Brokers)
	}
	if cluster.Replication != 1 || cluster.TLS || cluster.SASL != nil {
		t.Errorf("want plaintext, unauthenticated, one replica; got %+v", cluster)
	}
	if got := len(cluster.Options()); got != 1 {
		t.Errorf("want only the seed brokers as options, got %d", got)
	}
}

// A production cluster: three replicas, TLS, SCRAM. Every piece has to arrive,
// or the first client built from it fails in a way that looks like a network
// problem.
func TestClusterFromEnvReadsARealCluster(t *testing.T) {
	clearKafkaEnv(t)
	t.Setenv("KAFKA_BROKERS", "a:9093, b:9093,c:9093")
	t.Setenv("KAFKA_REPLICATION_FACTOR", "3")
	t.Setenv("KAFKA_TLS", "true")
	t.Setenv("KAFKA_SASL_MECHANISM", "SCRAM-SHA-512")
	t.Setenv("KAFKA_SASL_USERNAME", "surge")
	t.Setenv("KAFKA_SASL_PASSWORD", "secret")

	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(cluster.Brokers) != 3 || cluster.Brokers[1] != "b:9093" {
		t.Errorf("brokers = %v", cluster.Brokers)
	}
	if cluster.Replication != 3 || !cluster.TLS {
		t.Errorf("got %+v", cluster)
	}
	if cluster.SASL == nil || cluster.SASL.Name() != "SCRAM-SHA-512" {
		t.Errorf("sasl = %v, want SCRAM-SHA-512", cluster.SASL)
	}
	if got := len(cluster.Options()); got != 3 {
		t.Errorf("want seeds, TLS and SASL as options, got %d", got)
	}
}

// Set but wrong is never downgraded to a default: a replication factor of zero
// or a misspelt mechanism must stop the service, not start it unauthenticated
// or with topics that lose data.
func TestClusterFromEnvRefusesWhatItCannotUse(t *testing.T) {
	cases := map[string]struct {
		env     map[string]string
		invalid bool
	}{
		"replication zero":      {env: map[string]string{"KAFKA_REPLICATION_FACTOR": "0"}, invalid: true},
		"replication not a num": {env: map[string]string{"KAFKA_REPLICATION_FACTOR": "three"}, invalid: true},
		"tls not a bool":        {env: map[string]string{"KAFKA_TLS": "yes please"}, invalid: true},
		"unknown mechanism": {env: map[string]string{
			"KAFKA_SASL_MECHANISM": "GSSAPI", "KAFKA_SASL_USERNAME": "u", "KAFKA_SASL_PASSWORD": "p",
		}, invalid: true},
		"mechanism without user": {env: map[string]string{
			"KAFKA_SASL_MECHANISM": "PLAIN", "KAFKA_SASL_PASSWORD": "p",
		}},
		"mechanism without password": {env: map[string]string{
			"KAFKA_SASL_MECHANISM": "PLAIN", "KAFKA_SASL_USERNAME": "u",
		}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			clearKafkaEnv(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := kafkax.ClusterFromEnv()
			var invalid *config.InvalidError
			var missing *config.MissingError
			switch {
			case tc.invalid && !errors.As(err, &invalid):
				t.Errorf("want an InvalidError, got %v", err)
			case !tc.invalid && !errors.As(err, &missing):
				t.Errorf("want a MissingError, got %v", err)
			}
		})
	}
}

func TestPlainMechanismIsNamed(t *testing.T) {
	clearKafkaEnv(t)
	t.Setenv("KAFKA_SASL_MECHANISM", "PLAIN")
	t.Setenv("KAFKA_SASL_USERNAME", "u")
	t.Setenv("KAFKA_SASL_PASSWORD", "p")

	cluster, err := kafkax.ClusterFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cluster.SASL.Name() != "PLAIN" {
		t.Errorf("sasl = %s, want PLAIN", cluster.SASL.Name())
	}
}
