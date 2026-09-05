package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/cryptography"
	postgresqladapter "github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/application/accounts"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"golang.org/x/term"
)

type recoveryInput struct {
	Email    string `json:"email"`
	Password string `json:"new_password"`
	Reason   string `json:"reason"`
}
type recoveryOptions struct {
	InputFile            string
	ConfirmAllWorkspaces bool
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stderr, os.Stdout); err != nil {
		message := "operation_failed"
		if errors.Is(err, domain.ErrInvalidInput) {
			message = "invalid_input"
		}
		if errors.Is(err, domain.ErrNotFound) {
			message = "account_not_found"
		}
		if errors.Is(err, domain.ErrUnauthenticated) || errors.Is(err, domain.ErrConflict) {
			message = "account_changed_retry_required"
		}
		fmt.Fprintln(os.Stderr, "recover-account:", message)
		os.Exit(1)
	}
}

func parseOptions(args []string) (recoveryOptions, error) {
	var options recoveryOptions
	flags := flag.NewFlagSet("recover-account", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.InputFile, "input-file", "", "owner-only recovery JSON file; otherwise hidden terminal prompts")
	flags.BoolVar(&options.ConfirmAllWorkspaces, "confirm-all-workspaces", false, "confirm global password replacement and session revocation")
	if flags.Parse(args) != nil || flags.NArg() != 0 || !options.ConfirmAllWorkspaces {
		return options, domain.ErrInvalidInput
	}
	return options, nil
}

func readRecoveryInput(path string, input *os.File, prompts io.Writer) (recoveryInput, error) {
	if path == "" {
		if !term.IsTerminal(int(input.Fd())) {
			return recoveryInput{}, domain.ErrInvalidInput
		}
		values := make([]string, 3)
		for index, label := range []string{"Login email", "New password (all workspaces)", "Recovery reason"} {
			fmt.Fprint(prompts, label+": ")
			value, err := term.ReadPassword(int(input.Fd()))
			fmt.Fprintln(prompts)
			if err != nil {
				clear(value)
				return recoveryInput{}, domain.ErrInvalidInput
			}
			values[index] = string(value)
			clear(value)
		}
		return recoveryInput{Email: values[0], Password: values[1], Reason: values[2]}, nil
	}
	payload, err := cryptography.ReadPrivateFile(path, 8192)
	if err != nil {
		return recoveryInput{}, domain.ErrInvalidInput
	}
	defer clear(payload)
	decoder := json.NewDecoder(bytes.NewReader(payload))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return recoveryInput{}, domain.ErrInvalidInput
	}
	var value recoveryInput
	fields := map[string]*string{"email": &value.Email, "new_password": &value.Password, "reason": &value.Reason}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return recoveryInput{}, domain.ErrInvalidInput
		}
		name, ok := key.(string)
		target, exists := fields[name]
		if !ok || !exists || seen[name] {
			return recoveryInput{}, domain.ErrInvalidInput
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || string(raw) == "null" || json.Unmarshal(raw, target) != nil {
			return recoveryInput{}, domain.ErrInvalidInput
		}
		seen[name] = true
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(seen) != 3 {
		return recoveryInput{}, domain.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return recoveryInput{}, domain.ErrInvalidInput
	}
	return value, nil
}

func run(args []string, input *os.File, prompts, output io.Writer) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	values, err := readRecoveryInput(options.InputFile, input, prompts)
	if err != nil {
		return err
	}
	email, err := domain.NormalizeLoginEmail(values.Email)
	if err != nil {
		return err
	}
	password := []byte(values.Password)
	defer clear(password)
	values.Password = ""
	config, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := postgresqladapter.Open(ctx, config)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.CheckObjectRoot(ctx, os.Getenv("SBM_OBJECTS_PATH")); err != nil {
		return err
	}
	user, err := store.FindAccountByEmail(ctx, email)
	if err != nil {
		return err
	}
	hasher, err := cryptography.NewPasswordHasher(cryptography.DefaultArgon2Params)
	if err != nil {
		return err
	}
	service := accounts.NewService(store, hasher, cryptography.TokenGenerator{}, system.IDGenerator{}, system.Clock{})
	if err := service.Recover(ctx, user.ID, password, values.Reason); err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, "account recovered; all workspace sessions revoked")
	return err
}
