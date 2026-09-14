package tags

import "testing"

func TestATagIsFourCharactersOfTheStacksAlphabet(t *testing.T) {
	for _, c := range []struct {
		tag  Tag
		want bool
	}{
		{"03QK", true},
		{"0A3F", true},
		{"ZZ09", true},
		{"0000", true},
		{"03Q", false},
		{"03QKA", false},
		{"03qk", false},
		{"03-K", false},
		{"", false},
	} {
		if got := c.tag.Valid(); got != c.want {
			t.Errorf("%q came back %v", c.tag, got)
		}
	}
}

func TestEveryTagHandedOutIsATag(t *testing.T) {
	s := NewSpace("2311.05762", nil)
	for i := 0; i < 500; i++ {
		if tag := s.Next(); !tag.Valid() {
			t.Fatalf("handed out %q, which is not a tag", tag)
		}
	}
}

func TestOnePaperNeverHandsOutTheSameTagTwice(t *testing.T) {
	s := NewSpace("2311.05762", nil)
	seen := map[Tag]bool{}
	for i := 0; i < 2000; i++ {
		tag := s.Next()
		if seen[tag] {
			t.Fatalf("%s was handed out twice", tag)
		}
		seen[tag] = true
	}
}

// The order has to be the same on every run or a re-extraction of an unchanged
// paper writes different bytes, and a corpus that churns on every run is a
// corpus nobody can review a diff of.
func TestThePaperHandsOutItsTagsInTheSameOrderEveryTime(t *testing.T) {
	first, second := NewSpace("2311.05762", nil), NewSpace("2311.05762", nil)
	for i := 0; i < 50; i++ {
		if a, b := first.Next(), second.Next(); a != b {
			t.Fatalf("the %dth tag was %s and then %s", i, a, b)
		}
	}
}

// Two papers side by side should have nothing in common, because the first
// person to see a pattern across papers will write code that depends on it.
func TestTwoPapersDoNotHandOutTheSameTagsInTheSameOrder(t *testing.T) {
	a, b := NewSpace("2311.05762", nil), NewSpace("2404.19756", nil)
	same := 0
	for i := 0; i < 50; i++ {
		if a.Next() == b.Next() {
			same++
		}
	}
	if same > 1 {
		t.Fatalf("%d of the first 50 tags matched between two papers", same)
	}
}

// Nothing about a tag says where its object sits, which is the point of drawing
// from a shuffled space. If tags came out in order then sorting by tag would
// sort by position, and that would work until somebody inserted a section.
func TestTagsDoNotComeOutInOrder(t *testing.T) {
	s := NewSpace("2311.05762", nil)
	ascending := 0
	prev := s.Next()
	for i := 0; i < 50; i++ {
		next := s.Next()
		if next > prev {
			ascending++
		}
		prev = next
	}
	if ascending > 40 || ascending < 10 {
		t.Fatalf("%d of 50 tags were larger than the one before, which is not a shuffle", ascending)
	}
}

func TestATagAlreadyInTheRegisterIsNotHandedOutAgain(t *testing.T) {
	first := NewSpace("2311.05762", nil).Next()
	s := NewSpace("2311.05762", []Tag{first})
	if got := s.Next(); got == first {
		t.Fatalf("handed out %s again, and a reused tag points every old reference at a new object", got)
	}
}
