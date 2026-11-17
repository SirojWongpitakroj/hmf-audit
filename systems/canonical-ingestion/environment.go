package canonicalingestion

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// LoadEnv loads a local deployment profile without overwriting inherited
// environment variables. Only whole-value ${NAME} references are expanded;
// dollar signs inside passwords are never interpreted.
func LoadEnv(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		key, value, ok := strings.Cut(text, "=")
		if !ok {
			return fmt.Errorf("environment profile line %d: expected KEY=VALUE", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || strings.ContainsAny(key, " \t\r\n") {
			return fmt.Errorf("environment profile line %d: invalid variable", line)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'')) {
			value = value[1 : len(value)-1]
		}
		if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && strings.Count(value, "${") == 1 {
			reference := value[2 : len(value)-1]
			var exists bool
			value, exists = os.LookupEnv(reference)
			if !exists {
				return fmt.Errorf("environment profile line %d: reference %s is unset", line, reference)
			}
		}
		if err = os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func PostgresDSN(prefix, fallback string) (string, error) {
	if dsn := os.Getenv(prefix + "_DSN"); dsn != "" {
		return dsn, nil
	}
	if fallback != "" && os.Getenv(fallback+"_DSN") != "" {
		override := false
		for _, suffix := range []string{"HOST", "PORT", "DATABASE", "USER", "PASSWORD", "SSLMODE"} {
			if os.Getenv(prefix+"_"+suffix) != "" {
				override = true
				break
			}
		}
		if !override {
			return os.Getenv(fallback + "_DSN"), nil
		}
	}
	get := func(suffix string) string {
		v := os.Getenv(prefix + "_" + suffix)
		if v == "" && fallback != "" {
			v = os.Getenv(fallback + "_" + suffix)
		}
		return v
	}
	host, database, user, password := get("HOST"), get("DATABASE"), get("USER"), get("PASSWORD")
	if host == "" || database == "" || user == "" || password == "" {
		return "", fmt.Errorf("%s_DSN or HOST/DATABASE/USER/PASSWORD configuration required (optional fallback %s)", prefix, fallback)
	}
	port := get("PORT")
	if port == "" {
		port = "5432"
	}
	ssl := get("SSLMODE")
	if ssl == "" {
		ssl = "require"
	}
	u := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port), Path: "/" + database, User: url.UserPassword(user, password)}
	q := url.Values{}
	q.Set("sslmode", ssl)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
func OpenEnvPostgres(ctx context.Context, prefix, fallback string, connections int) (*sql.DB, error) {
	dsn, err := PostgresDSN(prefix, fallback)
	if err != nil {
		return nil, err
	}
	return OpenPostgres(ctx, dsn, connections)
}
