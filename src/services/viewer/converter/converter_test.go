package converter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ivenkwan/evdr/src/services/viewer/converter"
)

func TestLibreOfficeSupportsOfficeMimes(t *testing.T) {
	c := converter.NewLibreOffice("/usr/bin/soffice")
	for _, ct := range []string{
		"application/msword",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"application/vnd.oasis.opendocument.text",
		"application/rtf",
	} {
		if !c.Supports(ct) {
			t.Errorf("Supports(%q) = false", ct)
		}
	}
	for _, ct := range []string{"application/pdf", "text/plain", "image/png", ""} {
		if c.Supports(ct) {
			t.Errorf("Supports(%q) = true, want false", ct)
		}
	}
}

func TestLibreOfficeStubNeverConverts(t *testing.T) {
	c := converter.NewLibreOffice("/usr/bin/soffice")
	// The stub must fail loudly and honestly — no fake PDFs, no exec.
	out, err := c.ConvertToPDF(context.Background(), []byte("fake docx bytes"), "deal.docx")
	if out != nil {
		t.Error("stub must not return content")
	}
	if !errors.Is(err, converter.ErrUnavailable) {
		t.Fatalf("error = %v, want ErrUnavailable", err)
	}
	if err == nil || err.Error() == "" {
		t.Fatal("error must carry an explanatory message")
	}
}

func TestLibreOfficeHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := converter.NewLibreOffice("/usr/bin/soffice")
	if _, err := c.ConvertToPDF(ctx, []byte("x"), "a.docx"); !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}
