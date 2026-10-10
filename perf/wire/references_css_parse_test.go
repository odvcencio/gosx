package wire

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestReferencesNestedCSSSelectorsKeepLoads(t *testing.T) {
	for _, selector := range []string{":global(html:has(.card))", ":has(:is(.card, .tile))", "&:has(.card)"} {
		t.Run(selector, func(t *testing.T) {
			body := []byte(`@import url("/theme.css");.outer{` + selector + `{background:url("/nested.png");mask-image:image-set("/one.webp" 1x,url("/two.webp") 2x)}}`)
			set, err := ScanReferences(body, KindStyle)
			want := []Reference{{URL: "/nested.png", Kind: KindImage}, {URL: "/one.webp", Kind: KindImage}, {URL: "/theme.css", Kind: KindStyle}, {URL: "/two.webp", Kind: KindImage}}
			if err != nil || !reflect.DeepEqual(referenceValues(set.Resources), want) {
				t.Fatalf("nested selectors lost declared loads: %+v %v", set, err)
			}
		})
	}
}

func TestReferencesInvalidCSSIsIncomplete(t *testing.T) {
	for _, body := range []string{`.broken { color: }`, `.a{background:url('unfinished)`, ".a{color:\xff}"} {
		set, err := ScanAssetReferences([]byte(body), KindStyle, "app/fixture/public/style.css")
		if err != nil || set.Complete || len(set.Drops) != 1 {
			t.Fatalf("unparsed CSS must be incomplete without a hard error: %+v %v", set, err)
		}
		drop := set.Drops[0]
		if drop.Reason != "css-parse" || drop.File != "app/fixture/public/style.css" || drop.Offset < 0 || drop.Offset > int64(len(body)) {
			t.Fatalf("missing logical source and byte offset: %+v", drop)
		}
	}
	set, err := ScanAssetReferences([]byte(`.broken { color: }`), KindStyle, "app/fixture/public/style.css")
	if err != nil || set.Drops[0].Offset != 16 {
		t.Fatalf("wrong declaration failure offset: %+v %v", set, err)
	}
}

func TestReferencesBeaconCSSParserGapIsIncomplete(t *testing.T) {
	body, err := os.ReadFile("../../examples/gosx-docs/app/demos/beacon/page.css")
	if err != nil {
		t.Fatal(err)
	}
	set, err := ScanAssetReferences(body, KindStyle, "app/docs/css/app/demos/beacon/page.css")
	if err != nil || set.Complete || len(set.Drops) == 0 || set.Drops[0].Reason != "css-parse" || set.Drops[0].File != "app/docs/css/app/demos/beacon/page.css" {
		t.Fatalf("shipped nested selector must keep conservative coverage: %+v %v", set, err)
	}
}

func TestReferencesMalformedInlineCSSIsIncomplete(t *testing.T) {
	for _, body := range []string{`<style>.broken { color: }</style>`, `<p style="color:">ok</p>`} {
		set, err := ScanReferences([]byte(body), KindDocument)
		if err != nil || set.Complete {
			t.Fatalf("inline CSS parse failure must keep uncertainty: %+v %v", set, err)
		}
	}
}

func TestReferencesCSSGapDoesNotMutateBody(t *testing.T) {
	body := []byte(strings.Repeat(":global(html:has(.card)){background:url('/image.png')}\n", 2))
	before := string(body)
	set, err := ScanReferences(body, KindStyle)
	if err != nil || string(body) != before || len(set.Resources) != 1 {
		t.Fatalf("selector recovery changed bytes or lost references: %+v %v", set, err)
	}
}
