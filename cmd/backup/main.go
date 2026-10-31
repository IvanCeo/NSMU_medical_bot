package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/IvanCeo/NSMU_medical_bot/internal/backup"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Ошибка резервного копирования:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	command := "create"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(output)
	source := flags.String("source", "/data", "stopped bot data directory")
	destination := flags.String("destination", "/backups", "archive output or empty restore directory")
	archive := flags.String("archive", "", "archive to verify or restore")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %d", flags.NArg())
	}
	switch command {
	case "create":
		name, err := backup.Create(ctx, *source, *destination)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, "Копия создана:", name)
		return err
	case "verify":
		if err := backup.Verify(ctx, *archive); err != nil {
			return err
		}
	case "restore":
		if err := backup.Restore(ctx, *archive, *destination); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown command %q (use create, verify or restore)", command)
	}
	_, err := fmt.Fprintln(output, "Готово:", command)
	return err
}
