package emoji

// Alias maps a compatibility shortcode to a canonical shortcode that already
// exists in the generated emoji table.
type Alias struct {
	Name   string
	Target string
}

// SlackishAliases covers common Slack-style, iamcal, and descriptive aliases
// that are not already present in the generated GitHub/Unicode table.
var SlackishAliases = []Alias{
	{Name: "check_mark", Target: "white_check_mark"},
	{Name: "cooking", Target: "fried_egg"},
	{Name: "face_with_finger_covering_closed_lips", Target: "shushing_face"},
	{Name: "flag-cn", Target: "cn"},
	{Name: "flag-de", Target: "de"},
	{Name: "flag-es", Target: "es"},
	{Name: "flag-fr", Target: "fr"},
	{Name: "flag-gb", Target: "gb"},
	{Name: "flag-it", Target: "it"},
	{Name: "flag-jp", Target: "jp"},
	{Name: "flag-kr", Target: "kr"},
	{Name: "flag-ru", Target: "ru"},
	{Name: "flag-us", Target: "us"},
	{Name: "grinning_face_with_one_large_and_one_small_eye", Target: "zany_face"},
	{Name: "hand_with_index_and_middle_fingers_crossed", Target: "crossed_fingers"},
	{Name: "ladybug", Target: "lady_beetle"},
	{Name: "man_and_woman_holding_hands", Target: "couple"},
	{Name: "men_holding_hands", Target: "two_men_holding_hands"},
	{Name: "mother_christmas", Target: "mrs_claus"},
	{Name: "partly_sunny_rain", Target: "sun_behind_rain_cloud"},
	{Name: "person_frowning", Target: "frowning_person"},
	{Name: "person_with_blond_hair", Target: "blond_haired_person"},
	{Name: "person_with_pouting_face", Target: "pouting_face"},
	{Name: "red_heart", Target: "heart"},
	{Name: "reversed_hand_with_middle_finger_extended", Target: "middle_finger"},
	{Name: "shocked_face_with_exploding_head", Target: "exploding_head"},
	{Name: "simple_smile", Target: "slightly_smiling_face"},
	{Name: "slight_smile", Target: "slightly_smiling_face"},
	{Name: "squirrel", Target: "chipmunk"},
	{Name: "staff_of_aesculapius", Target: "medical_symbol"},
	{Name: "thumbs_down", Target: "thumbsdown"},
	{Name: "thumbs_up", Target: "thumbsup"},
	{Name: "tornado_cloud", Target: "tornado"},
	{Name: "woman_and_man_holding_hands", Target: "couple"},
	{Name: "women_holding_hands", Target: "two_women_holding_hands"},
}

// ApplyAliases adds compatibility aliases to a shortcode table. Existing
// entries win over aliases so generated canonical names cannot be overwritten.
func ApplyAliases(table map[string]string) {
	for _, alias := range SlackishAliases {
		if _, exists := table[alias.Name]; exists {
			continue
		}
		if value, ok := table[alias.Target]; ok {
			table[alias.Name] = value
		}
	}
}
