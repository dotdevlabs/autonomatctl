// Package gen builds the autonomatctl command tree from the parsed OpenAPI
// contract. Nothing is mapped by hand: names derive from path and method,
// flags from the documented query parameters and body attributes.
package gen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dotdevlabs/ctlkit/pkg/clierror"
	"github.com/dotdevlabs/ctlkit/pkg/ctxutil"

	"github.com/dotdevlabs/autonomatctl/internal/apiclient"
	"github.com/dotdevlabs/autonomatctl/internal/render"
	"github.com/dotdevlabs/autonomatctl/internal/spec"
)

// Annotation keys set on every generated leaf command; the ai command and the
// conformance tests read them back.
const (
	AnnOperationID = "operationId"
	AnnMethod      = "method"
	AnnPath        = "path"
	AnnWebhook     = "webhook"

	// Flag annotations read by the ai command.
	FlagRequired = "autonomatctl.required"
	FlagEnum     = "autonomatctl.enum"
)

// reservedFlags are the global flags and the CLI-only flags; a contract field
// of the same name cannot be mirrored and fails generation loudly.
var reservedFlags = []string{"help", "json", "context", "format", "dry-run", "verbose", "all", "yes"}

// Config tunes generation.
type Config struct {
	// Reserved are top-level command names owned by someone else (ctlkit's
	// auth/context/version/ai, schema, help...).
	Reserved []string
	// Version is sent in the User-Agent.
	Version string
	// Tune adjusts the API client options (tests shorten the retry backoff).
	Tune func(*apiclient.Options)
}

// Build returns the top-level resource commands for every operation.
func Build(s *spec.Spec, cfg Config) ([]*cobra.Command, error) {
	b := &builder{cfg: cfg, groups: map[string]*cobra.Command{}}
	root := &cobra.Command{Use: "root"}
	b.groups[""] = root
	for _, op := range s.Operations {
		if err := b.add(op); err != nil {
			return nil, err
		}
	}
	hideEmptyGroups(root)
	return root.Commands(), nil
}

type builder struct {
	cfg    Config
	groups map[string]*cobra.Command
}

func (b *builder) add(op spec.Operation) error {
	group, verb, err := derive(op)
	if err != nil {
		return err
	}
	parent, err := b.group(group)
	if err != nil {
		return fmt.Errorf("%s: %w", op.ID, err)
	}
	name := verb
	if hasChild(parent, name) {
		name = kebab(op.ID)
		if hasChild(parent, name) {
			return fmt.Errorf("%s: command %q already exists under %q (and so does %q)",
				op.ID, verb, strings.Join(group, " "), name)
		}
	}
	cmd, err := b.leaf(name, op)
	if err != nil {
		return fmt.Errorf("%s: %w", op.ID, err)
	}
	parent.AddCommand(cmd)
	return nil
}

func hasChild(c *cobra.Command, name string) bool {
	for _, ch := range c.Commands() {
		if ch.Name() == name {
			return true
		}
	}
	return false
}

func (b *builder) group(path []string) (*cobra.Command, error) {
	cur := b.groups[""]
	for i, name := range path {
		key := strings.Join(path[:i+1], " ")
		if g, ok := b.groups[key]; ok {
			cur = g
			continue
		}
		if i == 0 {
			for _, r := range b.cfg.Reserved {
				if r == name {
					return nil, fmt.Errorf("resource %q collides with a built-in command", name)
				}
			}
		}
		if hasChild(cur, name) {
			return nil, fmt.Errorf("resource %q collides with a command of the same name", key)
		}
		g := &cobra.Command{
			Use:   name,
			Short: "Operations on " + key,
			Args:  cobra.ArbitraryArgs,
			RunE: func(c *cobra.Command, args []string) error {
				if len(args) > 0 {
					return clierror.New(clierror.CodeUsage,
						fmt.Sprintf("unknown command %q for %q", args[0], c.CommandPath()), "see '"+c.CommandPath()+" --help'")
				}
				return c.Help()
			},
		}
		cur.AddCommand(g)
		b.groups[key] = g
		cur = g
	}
	return cur, nil
}

func hideEmptyGroups(c *cobra.Command) bool {
	kids := c.Commands()
	if len(kids) == 0 {
		return c.Hidden
	}
	all := true
	for _, k := range kids {
		if !hideEmptyGroups(k) {
			all = false
		}
	}
	if all && c.Parent() != nil {
		c.Hidden = true
	}
	return all
}

// fieldKind is how a contract type is exposed as a flag.
type fieldKind int

const (
	kString fieldKind = iota
	kInt
	kNumber
	kBool
	kStrings
	kJSON
)

func kindOf(typ, item string) fieldKind {
	switch typ {
	case "string":
		return kString
	case "integer":
		return kInt
	case "number":
		return kNumber
	case "boolean":
		return kBool
	case "array":
		if item == "string" {
			return kStrings
		}
	}
	return kJSON
}

// input is one documented value the user can supply.
type input struct {
	name     string
	kind     fieldKind
	enum     []string
	required bool
	typ      string // contract type, for JSON validation
	query    bool
	// A required attribute whose default the contract documents needs no
	// flag: the default is sent when the flag is omitted.
	def     any
	sendDef bool
}

var space = regexp.MustCompile(`\s+`)

func usage(desc string, in input) string {
	u := space.ReplaceAllString(strings.TrimSpace(desc), " ")
	if len(u) > 160 {
		u = u[:157] + "..."
	}
	if in.kind == kJSON {
		u = strings.TrimSpace(u + " (JSON " + in.typ + ")")
	}
	if len(in.enum) > 0 {
		u = strings.TrimSpace(u + " (one of: " + strings.Join(in.enum, ", ") + ")")
	}
	if in.sendDef {
		d, _ := json.Marshal(in.def)
		u = strings.TrimSpace(u + " (default " + string(d) + ")")
	}
	if in.required {
		u = strings.TrimSpace(u + " (required)")
	}
	return u
}

func addFlag(fs *pflag.FlagSet, in input, desc string) {
	u := usage(desc, in)
	defer func() {
		if in.required {
			_ = fs.SetAnnotation(in.name, FlagRequired, []string{"true"})
		}
		if len(in.enum) > 0 {
			_ = fs.SetAnnotation(in.name, FlagEnum, in.enum)
		}
	}()
	switch in.kind {
	case kInt:
		fs.Int64(in.name, 0, u)
	case kNumber:
		fs.Float64(in.name, 0, u)
	case kBool:
		fs.Bool(in.name, false, u)
	case kStrings:
		fs.StringArray(in.name, nil, u)
	default:
		fs.String(in.name, "", u)
	}
}

func destructive(op spec.Operation) bool {
	if op.Method == "DELETE" {
		return true
	}
	s := strings.ToLower(op.Summary)
	return strings.Contains(s, "no way back") || strings.Contains(s, "irreversible")
}

func (b *builder) leaf(name string, op spec.Operation) (*cobra.Command, error) {
	use := name
	for _, p := range op.PathParams {
		use += " <" + p + ">"
	}
	short := op.Summary
	if short == "" {
		short = op.Method + " " + op.Path
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Long:  fmt.Sprintf("%s\n\n%s %s (operationId %s)", short, op.Method, op.Path, op.ID),
		Annotations: map[string]string{
			AnnOperationID: op.ID,
			AnnMethod:      op.Method,
			AnnPath:        op.Path,
		},
	}
	if op.Webhook() {
		cmd.Hidden = true
		cmd.Annotations[AnnWebhook] = "true"
		cmd.Long += "\n\nWebhook delivery: authenticates with the webhook token, not an operator API token."
	}
	want := len(op.PathParams)
	cmd.Args = func(_ *cobra.Command, args []string) error {
		if len(args) != want {
			return clierror.New(clierror.CodeUsage,
				fmt.Sprintf("%s takes %d argument(s) %s, got %d", name, want, argNames(op.PathParams), len(args)), "")
		}
		return nil
	}

	var inputs []input
	seen := map[string]bool{}
	claim := func(n string) error {
		for _, r := range reservedFlags {
			if r == n {
				return fmt.Errorf("contract field %q collides with the CLI flag --%s", n, n)
			}
		}
		if seen[n] {
			return fmt.Errorf("contract name %q is used twice (query and body)", n)
		}
		seen[n] = true
		return nil
	}
	fs := cmd.Flags()
	for _, q := range op.Query {
		if err := claim(q.Name); err != nil {
			return nil, err
		}
		in := input{name: q.Name, kind: kindOf(q.Type, "string"), required: q.Required, typ: q.Type, query: true}
		if in.kind == kJSON { // object-typed query parameters are sent as given
			in.kind = kString
		}
		inputs = append(inputs, in)
		addFlag(fs, in, q.Description)
	}
	var idInput *input
	if op.Body != nil && op.Body.Linkage {
		if err := claim(linkageFlag); err != nil {
			return nil, err
		}
		in := input{name: linkageFlag, kind: kStrings, required: true, typ: "array"}
		inputs = append(inputs, in)
		addFlag(fs, in, "Ids of the "+op.Body.DataType+" to link (repeatable)")
	} else if op.Body != nil {
		if op.Body.HasID {
			if err := claim("id"); err != nil {
				return nil, err
			}
			// The contract reuses one resource schema for requests and responses and
			// so marks id required even on create; the server is the judge of that.
			in := input{name: "id", kind: kString, typ: "string"}
			idInput = &in
			addFlag(fs, in, "Resource id (data.id)")
		}
		for _, f := range op.Body.Fields {
			if err := claim(f.Name); err != nil {
				return nil, err
			}
			in := input{name: f.Name, kind: kindOf(f.Type, f.ItemType), enum: f.Enum, required: f.Required, typ: f.Type}
			if f.Required && f.HasDefault {
				in.required, in.sendDef, in.def = false, true, f.Default
			}
			inputs = append(inputs, in)
			addFlag(fs, in, f.Description)
		}
	}
	if op.Method == "GET" && op.Collection {
		fs.Bool("all", false, "Follow links.next and merge every page into one document")
	}
	if destructive(op) {
		fs.Bool("yes", false, "Confirm this destructive operation (not sent to the API)")
	}

	cmd.RunE = func(c *cobra.Command, args []string) error {
		return b.run(c, op, idInput, inputs, args)
	}
	return cmd, nil
}

func argNames(ps []string) string {
	if len(ps) == 0 {
		return ""
	}
	return "<" + strings.Join(ps, "> <") + ">"
}

// linkageFlag is the flag carrying the resource identifiers of a relationship
// request.
const linkageFlag = "ids"

type linkageIdentifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type docBody struct {
	Data docData `json:"data"`
}

type docData struct {
	Type       string          `json:"type,omitempty"`
	ID         *string         `json:"id,omitempty"`
	Attributes *map[string]any `json:"attributes,omitempty"`
}

// request assembles the API request from the arguments and the flags the user
// set. Only documented names can occur: they are the only flags there are.
func request(c *cobra.Command, op spec.Operation, idIn *input, inputs []input, args []string) (apiclient.Request, error) {
	req := apiclient.Request{Method: op.Method, Path: op.Path}
	for i, p := range op.PathParams {
		if args[i] == "" {
			return req, clierror.New(clierror.CodeUsage, "empty value for <"+p+">", "")
		}
		req.Path = strings.Replace(req.Path, "{"+p+"}", pathEscape(args[i]), 1)
	}

	var missing []string
	attrs := map[string]any{}
	var id *string
	set := func(in input) (bool, any, error) {
		if !c.Flags().Changed(in.name) {
			if in.required {
				missing = append(missing, "--"+in.name)
			}
			return false, nil, nil
		}
		v, err := value(c.Flags(), in)
		return err == nil, v, err
	}
	query := map[string][]string{}
	for _, in := range inputs {
		ok, v, err := set(in)
		if err != nil {
			return req, err
		}
		if !ok {
			if in.sendDef {
				attrs[in.name] = in.def
			}
			continue
		}
		if in.query {
			query[in.name] = queryValues(v)
		} else {
			attrs[in.name] = v
		}
	}
	if idIn != nil {
		if ok, v, err := set(*idIn); err != nil {
			return req, err
		} else if ok {
			s := v.(string)
			id = &s
		}
	}
	if len(missing) > 0 {
		return req, clierror.New(clierror.CodeUsage, "missing required flag(s): "+strings.Join(missing, ", "), "")
	}
	if len(query) > 0 {
		req.Query = query
	}
	if op.Body != nil && op.Body.Linkage {
		ids, _ := attrs[linkageFlag].([]string)
		list := make([]linkageIdentifier, 0, len(ids))
		for _, id := range ids {
			list = append(list, linkageIdentifier{Type: op.Body.DataType, ID: id})
		}
		body, err := json.Marshal(map[string]any{"data": list})
		if err != nil {
			return req, err
		}
		req.Body = body
		req.ContentType = op.Body.ContentType
	} else if op.Body != nil && (op.Body.DataType != "" || id != nil || len(op.Body.Fields) > 0) {
		d := docData{Type: op.Body.DataType, ID: id}
		if len(op.Body.Fields) > 0 {
			d.Attributes = &attrs
		}
		body, err := json.Marshal(docBody{Data: d})
		if err != nil {
			return req, err
		}
		req.Body = body
		req.ContentType = op.Body.ContentType
	}
	return req, nil
}

func pathEscape(s string) string { return url.PathEscape(s) }

func queryValues(v any) []string {
	if ss, ok := v.([]string); ok {
		return ss
	}
	if f, ok := v.(float64); ok {
		return []string{strconv.FormatFloat(f, 'f', -1, 64)}
	}
	return []string{fmt.Sprint(v)}
}

// value reads a changed flag as its contract type, validating enums and JSON.
func value(fs *pflag.FlagSet, in input) (any, error) {
	bad := func(msg string) error {
		return clierror.New(clierror.CodeUsage, fmt.Sprintf("--%s: %s", in.name, msg), "")
	}
	switch in.kind {
	case kInt:
		return fs.GetInt64(in.name)
	case kNumber:
		return fs.GetFloat64(in.name)
	case kBool:
		v, _ := fs.GetBool(in.name)
		if in.query {
			return strconv.FormatBool(v), nil
		}
		return v, nil
	case kStrings:
		v, _ := fs.GetStringArray(in.name)
		return v, nil
	case kJSON:
		raw, _ := fs.GetString(in.name)
		if !json.Valid([]byte(raw)) {
			return nil, bad("not valid JSON")
		}
		first := strings.TrimSpace(raw)[:1]
		if (in.typ == "array" && first != "[" && first != "n") || (in.typ == "object" && first != "{" && first != "n") {
			return nil, bad("expected a JSON " + in.typ)
		}
		return json.RawMessage(raw), nil
	}
	v, _ := fs.GetString(in.name)
	if len(in.enum) > 0 && !contains(in.enum, v) {
		return nil, bad(fmt.Sprintf("%q is not one of: %s", v, strings.Join(in.enum, ", ")))
	}
	return v, nil
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func (b *builder) run(c *cobra.Command, op spec.Operation, idIn *input, inputs []input, args []string) error {
	req, err := request(c, op, idIn, inputs, args)
	if err != nil {
		return err
	}
	ctx := c.Context()
	gf := ctxutil.GlobalFlagsFrom(ctx)
	ac := ctxutil.ActiveContextFrom(ctx)
	if ac == nil {
		return clierror.New(clierror.CodeUsage, "no active context",
			"use 'auth login --name <ctx>' or set AUTONOMATCTL_BASE_URL and AUTONOMATCTL_TOKEN")
	}
	base, err := apiclient.ResolveBaseURL(ac.BaseURL)
	if err != nil {
		return err
	}
	opts := apiclient.Options{BaseURL: base, Token: ac.Token, UserAgent: "autonomatctl/" + b.cfg.Version}
	if gf.Verbose {
		opts.Verbose = c.ErrOrStderr()
	}
	if b.cfg.Tune != nil {
		b.cfg.Tune(&opts)
	}
	client, err := apiclient.New(opts)
	if err != nil {
		return err
	}
	ro := render.Options{JSON: gf.JSON, Format: gf.Format}
	out := c.OutOrStdout()

	if gf.DryRun {
		info := render.DryRunInfo{Method: req.Method, URL: client.URL(req), Headers: client.Headers(req)}
		if len(req.Body) > 0 {
			info.Body = req.Body
		}
		return render.DryRun(out, ro, info)
	}
	if y := c.Flags().Lookup("yes"); y != nil && y.Value.String() != "true" {
		return clierror.New(clierror.CodeUsage, op.Method+" "+op.Path+" is destructive", "re-run with --yes to confirm (or --dry-run to preview)")
	}

	var resp *apiclient.Response
	if all, _ := c.Flags().GetBool("all"); all {
		resp, err = client.FetchAll(ctx, req)
	} else {
		resp, err = client.Do(ctx, req)
	}
	if err != nil {
		return err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return render.Failure(out, ro, resp.Status, resp.Body)
	}
	return render.Success(out, c.ErrOrStderr(), ro, resp.Status, resp.Body)
}
