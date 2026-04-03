package config

import (
	"errors"
	"fmt"

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
}

// Validate checks whether the parsed config is usable by the proxy.
func Validate(cfg Config) error {
	if err := playgroundValidator.Struct(cfg); err != nil {
		return mapValidationError(err)
	}

	if len(cfg.IFaces) == 0 {
		return errors.New("ifaces must contain at least one interface")
	}

	for name, iface := range cfg.IFaces {
		if name == "" {
			return errors.New("ifaces cannot contain an empty interface name")
		}

		if iface.Weight <= 0 {
			return fmt.Errorf("interface %q must have weight greater than 0", name)
		}

		if iface.SourceIP == nil {
			continue
		}
		if iface.SourceIP.IsUnspecified() {
			return fmt.Errorf("interface %q source_ip cannot be unspecified", name)
		}
		if iface.SourceIP.IsLoopback() {
			return fmt.Errorf("interface %q source_ip cannot be loopback", name)
		}
		if !iface.SourceIP.IsGlobalUnicast() {
			return fmt.Errorf("interface %q source_ip must be a unicast address", name)
		}
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
		case "Weight":
			return errors.New("interface weight must be greater than 0")
		}
	}

	return err
}
