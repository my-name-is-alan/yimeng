package library

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrBadRules = errors.New("invalid smart playlist rules")

// Rules define a smart playlist. Example:
//
//	{"match":"all","rules":[{"field":"genre","op":"contains","value":"jazz"},
//	 {"field":"added","op":"in_last","value":30}],"sort":"added","order":"desc","limit":100}
type Rules struct {
	Match string `json:"match"` // all | any
	Rules []Rule `json:"rules"`
	Sort  string `json:"sort,omitempty"`  // any track sort (title, added, plays, last_played, rating, random, ...)
	Order string `json:"order,omitempty"` // asc | desc
	Limit int    `json:"limit,omitempty"` // ≤ 5000; default 500
}

type Rule struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value"`
}

type fieldKind int

const (
	kStr fieldKind = iota
	kNum
	kDate
	kBool
)

// Only these SQL expressions are ever interpolated; values are bound parameters.
var smartFields = map[string]struct {
	expr string
	kind fieldKind
}{
	"title":       {"t.title", kStr},
	"artist":      {"COALESCE(t.artist_name,'')", kStr},
	"album":       {"COALESCE(al.title,'')", kStr},
	"albumartist": {"COALESCE(al.artist_name,'')", kStr},
	"genre":       {"COALESCE(t.genre,'')", kStr},
	"format":      {"COALESCE(st.format,'')", kStr},
	"codec":       {"COALESCE(st.codec,'')", kStr},
	"year":        {"COALESCE(t.year,0)", kNum},
	"duration":    {"COALESCE(t.duration_ms,0)/1000", kNum},
	"plays":       {"COALESCE(ts.play_count,0)", kNum},
	"rating":      {"COALESCE(rt.rating,0)", kNum},
	"sample_rate": {"COALESCE(st.sample_rate,0)", kNum},
	"bit_depth":   {"COALESCE(st.bit_depth,0)", kNum},
	"added":       {"t.created_at", kDate},
	"last_played": {"COALESCE(ts.last_played_at,0)", kDate},
	"favorite":    {"(fv.entity_id IS NOT NULL)", kBool},
	"lossless":    {"COALESCE(st.lossless,0)", kBool},
}

// Compile validates rules and returns a WHERE fragment, its args, ORDER BY and limit.
func (r Rules) Compile(now time.Time) (string, []any, string, int, error) {
	bad := func(f string, a ...any) (string, []any, string, int, error) {
		return "", nil, "", 0, fmt.Errorf("%w: "+f, append([]any{ErrBadRules}, a...)...)
	}
	join := " AND "
	switch r.Match {
	case "", "all":
	case "any":
		join = " OR "
	default:
		return bad("match must be all|any")
	}
	if len(r.Rules) > 50 {
		return bad("at most 50 rules")
	}
	var conds []string
	var args []any
	for i, ru := range r.Rules {
		f, ok := smartFields[ru.Field]
		if !ok {
			return bad("rule %d: unknown field %q", i, ru.Field)
		}
		var str string
		var num float64
		var pair [2]float64
		var b bool
		switch f.kind {
		case kStr:
			if json.Unmarshal(ru.Value, &str) != nil || len(str) > 200 {
				return bad("rule %d: value must be a string", i)
			}
			esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(str))
			switch ru.Op {
			case "is":
				conds, args = append(conds, "lower("+f.expr+")=?"), append(args, strings.ToLower(str))
			case "is_not":
				conds, args = append(conds, "lower("+f.expr+")<>?"), append(args, strings.ToLower(str))
			case "contains":
				conds, args = append(conds, "lower("+f.expr+`) LIKE ? ESCAPE '\'`), append(args, "%"+esc+"%")
			case "not_contains":
				conds, args = append(conds, "lower("+f.expr+`) NOT LIKE ? ESCAPE '\'`), append(args, "%"+esc+"%")
			case "starts_with":
				conds, args = append(conds, "lower("+f.expr+`) LIKE ? ESCAPE '\'`), append(args, esc+"%")
			default:
				return bad("rule %d: op for text must be is|is_not|contains|not_contains|starts_with", i)
			}
		case kNum:
			if ru.Op == "between" {
				if json.Unmarshal(ru.Value, &pair) != nil {
					return bad("rule %d: between needs [min,max]", i)
				}
				conds, args = append(conds, f.expr+" BETWEEN ? AND ?"), append(args, pair[0], pair[1])
				continue
			}
			if json.Unmarshal(ru.Value, &num) != nil {
				return bad("rule %d: value must be a number", i)
			}
			op, ok := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}[ru.Op]
			if !ok {
				return bad("rule %d: op for numbers must be eq|ne|gt|gte|lt|lte|between", i)
			}
			conds, args = append(conds, f.expr+op+"?"), append(args, num)
		case kDate:
			switch ru.Op {
			case "in_last", "not_in_last":
				if json.Unmarshal(ru.Value, &num) != nil || num <= 0 {
					return bad("rule %d: value must be a positive number of days", i)
				}
				cut := now.Add(-time.Duration(num * float64(24*time.Hour))).UnixMilli()
				op := ">="
				if ru.Op == "not_in_last" {
					op = "<"
				}
				conds, args = append(conds, f.expr+op+"?"), append(args, cut)
			case "before", "after":
				var ts time.Time
				if json.Unmarshal(ru.Value, &ts) != nil {
					return bad("rule %d: value must be an RFC 3339 time", i)
				}
				op := "<"
				if ru.Op == "after" {
					op = ">"
				}
				conds, args = append(conds, f.expr+op+"?"), append(args, ts.UnixMilli())
			default:
				return bad("rule %d: op for dates must be in_last|not_in_last|before|after", i)
			}
		case kBool:
			if ru.Op != "is" || json.Unmarshal(ru.Value, &b) != nil {
				return bad("rule %d: bool fields take op is with true|false", i)
			}
			if b {
				conds = append(conds, f.expr)
			} else {
				conds = append(conds, "NOT "+f.expr)
			}
		}
	}
	where := ""
	if len(conds) > 0 {
		where = "(" + strings.Join(conds, join) + ")"
	}
	order := ""
	if r.Sort == "random" {
		order = " ORDER BY random()"
	} else {
		p := ListParams{Sort: r.Sort, Desc: r.Order == "desc"}
		if r.Order != "" && r.Order != "asc" && r.Order != "desc" {
			return bad("order must be asc|desc")
		}
		var err error
		if order, err = orderBy(trackSorts, p, "title"); err != nil {
			return bad("unknown sort %q", r.Sort)
		}
	}
	limit := r.Limit
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		return bad("limit must be ≤ 5000")
	}
	return where, args, order, limit, nil
}
