package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var playgroundValidator = newPlaygroundValidator()

func newPlaygroundValidator() *validator.Validate {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterStructValidation(validateConfigStruct, Config{})
	return validate
}

func validateConfigStruct(structLevel validator.StructLevel) {
	cfg, ok := structLevel.Current().Interface().(Config)
	if !ok {
		return
	}

	if !cfg.Listen.IsValid() {
		structLevel.ReportError(cfg.Listen, "Listen", "listen", "addrport", "")
	}
	if !cfg.Server.IsValid() {
		structLevel.ReportError(cfg.Server, "Server", "server", "addrport", "")
	}
	if cfg.Metrics.IsValid() && cfg.Metrics == cfg.Listen {
		structLevel.ReportError(cfg.Metrics, "Metrics", "metrics", "different_from_listen", "")
	}
	if len(cfg.IFaces) == 0 {
		structLevel.ReportError(cfg.IFaces, "IFaces", "ifaces", "required", "")
	}

	usernameSet := strings.TrimSpace(cfg.Socks.Username) != ""
	passwordSet := strings.TrimSpace(cfg.Socks.Password) != ""
	if usernameSet != passwordSet {
		structLevel.ReportError(cfg.Socks, "Socks", "socks", "socks_pair", "")
	}

	for name, iface := range cfg.IFaces {
		if name == "" {
			structLevel.ReportError(cfg.IFaces, "IFaces", "ifaces", "iface_name_empty", "")
		}

		if iface.Weight <= 0 {
			structLevel.ReportError(iface.Weight, "Weight", "weight", "weight_gt", name)
		}

		if iface.SourceIP == nil {
			continue
		}
		if iface.SourceIP.IsUnspecified() {
			structLevel.ReportError(iface.SourceIP, "SourceIP", "source_ip", "source_unspecified", name)
		}
		if iface.SourceIP.IsLoopback() {
			structLevel.ReportError(iface.SourceIP, "SourceIP", "source_ip", "source_loopback", name)
		}
		if !iface.SourceIP.IsGlobalUnicast() {
			structLevel.ReportError(iface.SourceIP, "SourceIP", "source_ip", "source_unicast", name)
		}
	}
}

// Validate checks whether the parsed config is usable by the proxy.
func Validate(cfg Config) error {
	if err := playgroundValidator.Struct(cfg); err != nil {
		return mapValidationError(err)
	}

	return nil
}

// mapValidationError translates validator errors into user-facing config errors.
func mapValidationError(err error) error {
	var invalidValidation *validator.InvalidValidationError
	if errors.As(err, &invalidValidation) {
		return fmt.Errorf("invalid validation setup: %w", invalidValidation)
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return err
	}

	for _, validationErr := range validationErrs {
		switch validationErr.Tag() {
		case "addrport":
			if validationErr.Field() == "Listen" {
				return errors.New("listen must be a valid address:port")
			}
			if validationErr.Field() == "Server" {
				return errors.New("server must be a valid address:port")
			}
		case "different_from_listen":
			return errors.New("metrics must be different from listen")
		case "socks_pair":
			return errors.New("socks.username and socks.password must be set together")
		case "iface_name_empty":
			return errors.New("ifaces cannot contain an empty interface name")
		case "weight_gt":
			ifaceName := validationErr.Param()
			if ifaceName != "" {
				return fmt.Errorf("interface %q must have weight greater than 0", ifaceName)
			}
			return errors.New("interface weight must be greater than 0")
		case "source_unspecified":
			ifaceName := validationErr.Param()
			return fmt.Errorf("interface %q source_ip cannot be unspecified", ifaceName)
		case "source_loopback":
			ifaceName := validationErr.Param()
			return fmt.Errorf("interface %q source_ip cannot be loopback", ifaceName)
		case "source_unicast":
			ifaceName := validationErr.Param()
			return fmt.Errorf("interface %q source_ip must be a unicast address", ifaceName)
		}

		switch validationErr.Field() {
		case "Listen":
			return errors.New("listen must be a valid address:port")
		case "Server":
			return errors.New("server must be a valid address:port")
		case "Metrics":
			return errors.New("metrics must be different from listen")
		case "TTL":
			return errors.New("cache.ttl must be zero or greater")
		case "IFaces":
			return errors.New("ifaces must contain at least one interface")
		case "FailoverAttempts":
			return errors.New("failover_attempts must be zero or greater")
		case "Weight":
			return errors.New("interface weight must be greater than 0")
		}
	}

	return err
}
