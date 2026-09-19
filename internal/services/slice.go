package services

import "fmt"

// SliceRequest describes an array/pointer window the way source code writes it:
// buffer[37:52], data + item_offset, buffer[offset + i]. Exactly one of Length or
// End (exclusive) is normally given.
type SliceRequest struct {
	Base            string `json:"base"`
	BaseLength      *int   `json:"base_length,omitempty"` // in elements, when known
	Offset          int    `json:"offset"`
	Length          *int   `json:"length,omitempty"`
	End             *int   `json:"end,omitempty"` // exclusive
	ElementSize     int    `json:"element_size"`
	ElementType     string `json:"element_type,omitempty"`
	SourceReference string `json:"source_reference,omitempty"`
	Expression      string `json:"source_expression,omitempty"`
}

// SliceResult is the fully resolved window. All arithmetic is done here so the
// caller never has to carry offsets and lengths across reasoning steps.
type SliceResult struct {
	Base        string   `json:"base"`
	Offset      int      `json:"offset"`
	Length      int      `json:"length"`
	End         int      `json:"end"`        // exclusive
	LastIndex   int      `json:"last_index"` // inclusive, = End-1
	ElementSize int      `json:"element_size"`
	ByteOffset  int      `json:"byte_offset"`
	ByteLength  int      `json:"byte_length"`
	ByteEnd     int      `json:"byte_end"` // exclusive
	Notation    string   `json:"notation"`
	Valid       bool     `json:"valid"`
	Problems    []string `json:"problems,omitempty"`
	ElementType string   `json:"element_type,omitempty"`
	SourceRef   string   `json:"source_reference,omitempty"`
	SourceExpr  string   `json:"source_expression,omitempty"`
}

// ComputeSlice resolves a window and reports every inconsistency it can prove:
// negative values, length/end disagreement, and running past a known base length,
// including the classic off-by-one where an inclusive end was used as exclusive.
func ComputeSlice(req SliceRequest) SliceResult {
	res := SliceResult{Base: req.Base, Offset: req.Offset, ElementSize: req.ElementSize,
		ElementType: req.ElementType, SourceRef: req.SourceReference, SourceExpr: req.Expression}
	problem := func(f string, a ...any) { res.Problems = append(res.Problems, fmt.Sprintf(f, a...)) }
	if res.ElementSize <= 0 {
		res.ElementSize = 1
	}
	switch {
	case req.Length != nil && req.End != nil:
		res.Length, res.End = *req.Length, *req.End
		if req.Offset+*req.Length != *req.End {
			problem("length %d and end %d disagree: offset %d + length %d = %d (end is exclusive)",
				*req.Length, *req.End, req.Offset, *req.Length, req.Offset+*req.Length)
		}
	case req.Length != nil:
		res.Length = *req.Length
		res.End = req.Offset + res.Length
	case req.End != nil:
		res.End = *req.End
		res.Length = res.End - req.Offset
	default:
		problem("either length or end is required")
	}
	if req.Offset < 0 {
		problem("offset %d is negative", req.Offset)
	}
	if res.Length < 0 {
		problem("length %d is negative (end %d is before offset %d)", res.Length, res.End, req.Offset)
	}
	res.LastIndex = res.End - 1
	if req.BaseLength != nil {
		if res.End > *req.BaseLength {
			problem("window ends at %d but %s has %d elements: %d past the end", res.End, req.Base, *req.BaseLength, res.End-*req.BaseLength)
			if res.End-1 == *req.BaseLength {
				problem("off by one? if %d was an inclusive last index, the exclusive end is %d and the length is %d",
					res.End-1, res.End, res.Length)
			}
		}
		if req.Offset > *req.BaseLength {
			problem("offset %d is past the end of %s (%d elements)", req.Offset, req.Base, *req.BaseLength)
		}
	}
	res.ByteOffset = req.Offset * res.ElementSize
	res.ByteLength = res.Length * res.ElementSize
	res.ByteEnd = res.End * res.ElementSize
	res.Notation = fmt.Sprintf("%s[%d:%d] (half-open: %d element(s), indexes %d..%d; bytes %d..%d)",
		req.Base, req.Offset, res.End, res.Length, req.Offset, res.LastIndex, res.ByteOffset, res.ByteEnd-1)
	res.Valid = len(res.Problems) == 0
	return res
}
