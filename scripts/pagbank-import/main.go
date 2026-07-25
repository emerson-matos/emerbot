// Command pagbank-import turns PagBank EDI responses into a combined envelope
// and delivers it for import.
//
// It is deliberately the *only* way an envelope enters the system, local or
// deployed. The two -target values differ only in transport:
//
//	-target s3://bucket    uploads the envelope; the ObjectCreated event runs
//	                       the payment-importer Lambda, which parses and persists
//	-target dynamodb       runs that same importer in-process against
//	                       DYNAMODB_ENDPOINT
//
// Both paths call the identical apps/payment-importer app — same parser, same
// validation, same repository, same replace semantics. Nothing is reimplemented
// for local use, so a bug cannot hide on one side.
//
// Typical uses:
//
//	# Local: import a recorded scenario into dynamodb-local, dated this month.
//	go run ./scripts/pagbank-import -dir "cenarios/Cenário 1 - JSON" \
//	  -target dynamodb -rebase "$(date +%Y-%m)"
//
//	# Deployed: upload real EDI responses; the Lambda picks them up.
//	go run ./scripts/pagbank-import -dir ./extracts-2026-07-23 \
//	  -target s3://emerbot-dev-payment-imports
//
//	# Inspect the envelope without sending it anywhere.
//	go run ./scripts/pagbank-import -dir ./extracts -target - > envelope.json
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/emerson/emerbot/packages/domain"
	"github.com/emerson/emerbot/packages/payments"
	"github.com/emerson/emerbot/packages/payments/importer"
	"github.com/emerson/emerbot/packages/payments/pagbank"
	"github.com/emerson/emerbot/packages/shared"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Fatalf("pagbank-import: %v", err)
	}
}

type config struct {
	dir      string
	envelope string
	target   string
	date     string
	rebase   string
}

func run() error {
	var cfg config
	flag.StringVar(&cfg.dir, "dir", "", "directory of PagBank EDI response JSON files (transactional/financial/cashouts)")
	flag.StringVar(&cfg.envelope, "envelope", "", "path to an already-assembled envelope, instead of -dir")
	flag.StringVar(&cfg.target, "target", "", `where to deliver: "s3://bucket[/prefix]", "dynamodb", or "-" for stdout`)
	flag.StringVar(&cfg.date, "date", "", "source day of the import (YYYY-MM-DD); default: the newest date in the data")
	flag.StringVar(&cfg.rebase, "rebase", "", "shift all dates so the newest lands in this month (YYYY-MM); for demos against recorded scenarios")
	flag.Parse()

	if cfg.target == "" {
		flag.Usage()
		return errors.New("-target is required")
	}
	if (cfg.dir == "") == (cfg.envelope == "") {
		return errors.New("pass exactly one of -dir or -envelope")
	}

	raw, err := buildEnvelope(cfg)
	if err != nil {
		return err
	}

	ctx := context.Background()
	switch {
	case cfg.target == "-":
		_, err := os.Stdout.Write(raw)
		return err
	case strings.HasPrefix(cfg.target, "s3://"):
		return deliverToS3(ctx, cfg.target, raw)
	case cfg.target == "dynamodb":
		return importDirectly(ctx, raw)
	default:
		return fmt.Errorf("unknown -target %q (want s3://bucket, dynamodb, or -)", cfg.target)
	}
}

// buildEnvelope produces the envelope bytes, either by assembling EDI responses
// or by reading one that already exists.
func buildEnvelope(cfg config) ([]byte, error) {
	if cfg.envelope != "" {
		if cfg.rebase != "" || cfg.date != "" {
			return nil, errors.New("-date and -rebase only apply when assembling from -dir")
		}
		return os.ReadFile(cfg.envelope)
	}

	extracts, err := readExtracts(cfg.dir)
	if err != nil {
		return nil, err
	}
	if len(extracts) == 0 {
		return nil, fmt.Errorf("no EDI response files found in %s (expected names containing transactional/financial/cashouts)", cfg.dir)
	}

	var opts pagbank.AssembleOptions
	opts.RebaseTo = cfg.rebase
	if cfg.date != "" {
		d, err := domain.ParseCalendarDate(cfg.date)
		if err != nil {
			return nil, fmt.Errorf("invalid -date %q, expected YYYY-MM-DD: %w", cfg.date, err)
		}
		opts.SourceDate = d
	}

	raw, err := pagbank.Assemble(extracts, opts)
	if err != nil {
		return nil, err
	}
	log.Printf("assembled envelope from %d file(s) in %s", len(extracts), cfg.dir)
	return raw, nil
}

// readExtracts loads every recognisable EDI response in dir, in a stable order
// so the same directory always assembles to the same envelope.
func readExtracts(dir string) ([]pagbank.Extract, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var extracts []pagbank.Extract
	for _, name := range names {
		kind, ok := pagbank.ClassifyFile(name)
		if !ok {
			log.Printf("skipping %s: filename does not identify an extract", name)
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		extracts = append(extracts, pagbank.Extract{Kind: kind, Name: name, Data: data})
	}
	return extracts, nil
}

// deliverToS3 uploads the envelope under the prefix the bucket notification
// filters on, so the Lambda actually fires. The key carries the source date so
// re-uploading the same day overwrites its own object rather than piling up.
func deliverToS3(ctx context.Context, target string, raw []byte) error {
	bucket, prefix, _ := strings.Cut(strings.TrimPrefix(target, "s3://"), "/")
	if bucket == "" {
		return fmt.Errorf("malformed -target %q: missing bucket", target)
	}
	if prefix == "" {
		prefix = "imports"
	}
	prefix = strings.Trim(prefix, "/")
	if !strings.HasPrefix(prefix, "imports") {
		return fmt.Errorf("prefix %q would not trigger the importer: the bucket notification only fires under imports/", prefix)
	}

	sourceDate, err := envelopeSourceDate(raw)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("%s/pagbank/%s.json", prefix, sourceDate)

	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	if _, err := s3.NewFromConfig(cfg).PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(raw),
		ContentType: aws.String("application/json"),
	}); err != nil {
		return fmt.Errorf("put s3://%s/%s: %w", bucket, key, err)
	}
	log.Printf("uploaded s3://%s/%s — the payment-importer Lambda takes it from here", bucket, key)
	return nil
}

// importDirectly runs the importer in-process. This is the same Importer the Lambda
// builds; only the bytes' route differs.
func importDirectly(ctx context.Context, raw []byte) error {
	endpoint := shared.Getenv("DYNAMODB_ENDPOINT", "")
	if endpoint == "" {
		return errors.New("DYNAMODB_ENDPOINT is required for -target dynamodb (is the compose stack up?)")
	}
	table := shared.Getenv("FINANCIAL_ENTRIES_TABLE", "emerbot-local-financial-entries")

	repo, err := payments.NewDynamoDBRepository(ctx, table, endpoint, shared.FinanceLedgerID)
	if err != nil {
		return fmt.Errorf("payments repo: %w", err)
	}
	if err := importer.New(repo).ProcessRaw(ctx, raw); err != nil {
		return fmt.Errorf("import envelope: %w", err)
	}

	sourceDate, err := envelopeSourceDate(raw)
	if err != nil {
		return err
	}
	log.Printf("imported into %s (source day %s)", table, sourceDate)
	return nil
}

// envelopeSourceDate reads back the day the envelope covers, so the S3 key and
// the log line name the same day the repository will replace.
func envelopeSourceDate(raw []byte) (string, error) {
	var head struct {
		Date string `json:"date"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", fmt.Errorf("read envelope date: %w", err)
	}
	if head.Date == "" {
		return "", errors.New("envelope has no date")
	}
	if _, err := time.Parse("2006-01-02", head.Date); err != nil {
		return "", fmt.Errorf("envelope date %q is not YYYY-MM-DD", head.Date)
	}
	return head.Date, nil
}
