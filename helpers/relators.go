package helpers

import "strings"

// MARCRelator represents a MARC relator code and its label.
type MARCRelator struct {
	Code  string
	Label string
}

// MARCRelators maps MARC relator codes to human-readable labels.
// Source: Library of Congress Linked Data Service relators vocabulary
// (https://id.loc.gov/vocabulary/relators), fetched 2026-05-12.
// LOC labels are rendered with an initial capital to preserve existing display style.
var MARCRelators = map[string]string{
	"abr": "Abridger",
	"acp": "Art copyist",
	"act": "Actor",
	"adi": "Art director",
	"adp": "Adapter",
	"aft": "Author of afterword, colophon, etc.",
	"anc": "Announcer",
	"anl": "Analyst",
	"anm": "Animator",
	"ann": "Annotator",
	"ant": "Bibliographic antecedent",
	"ape": "Appellee",
	"apl": "Appellant",
	"app": "Applicant",
	"aqt": "Author in quotations or text abstracts",
	"arc": "Architect",
	"ard": "Artistic director",
	"arr": "Arranger",
	"art": "Artist",
	"asg": "Assignee",
	"asn": "Associated name",
	"ato": "Autographer",
	"att": "Attributed name",
	"auc": "Auctioneer",
	"aud": "Author of dialog",
	"aue": "Audio engineer",
	"aui": "Author of introduction, etc.",
	"aup": "Audio producer",
	"aus": "Screenwriter",
	"aut": "Author",
	"bdd": "Binding designer",
	"bjd": "Bookjacket designer",
	"bka": "Book artist",
	"bkd": "Book designer",
	"bkp": "Book producer",
	"blw": "Blurb writer",
	"bnd": "Binder",
	"bpd": "Bookplate designer",
	"brd": "Broadcaster",
	"brl": "Braille embosser",
	"bsl": "Bookseller",
	"cad": "Casting director",
	"cas": "Caster",
	"ccp": "Conceptor",
	"chr": "Choreographer",
	"cli": "Client",
	"cll": "Calligrapher",
	"clr": "Colorist",
	"clt": "Collotyper",
	"cmm": "Commentator",
	"cmp": "Composer",
	"cmt": "Compositor",
	"cnd": "Conductor",
	"cng": "Cinematographer",
	"cns": "Censor",
	"coe": "Contestant-appellee",
	"col": "Collector",
	"com": "Compiler",
	"con": "Conservator",
	"cop": "Camera operator",
	"cor": "Collection registrar",
	"cos": "Contestant",
	"cot": "Contestant-appellant",
	"cou": "Court governed",
	"cov": "Cover designer",
	"cpc": "Copyright claimant",
	"cpe": "Complainant-appellee",
	"cph": "Copyright holder",
	"cpl": "Complainant",
	"cpt": "Complainant-appellant",
	"cre": "Creator",
	"crp": "Correspondent",
	"crr": "Corrector",
	"crt": "Court reporter",
	"csl": "Consultant",
	"csp": "Consultant to a project",
	"cst": "Costume designer",
	"ctb": "Contributor",
	"cte": "Contestee-appellee",
	"ctg": "Cartographer",
	"ctr": "Contractor",
	"cts": "Contestee",
	"ctt": "Contestee-appellant",
	"cur": "Curator",
	"cwt": "Commentator for written text",
	"dbd": "Dubbing director",
	"dbp": "Distribution place",
	"dfd": "Defendant",
	"dfe": "Defendant-appellee",
	"dft": "Defendant-appellant",
	"dgc": "Degree committee member",
	"dgg": "Degree granting institution",
	"dgs": "Degree supervisor",
	"dis": "Dissertant",
	"djo": "Dj",
	"dln": "Delineator",
	"dnc": "Dancer",
	"dnr": "Donor",
	"dpc": "Depicted",
	"dpt": "Depositor",
	"drm": "Draftsman",
	"drt": "Director",
	"dsr": "Designer",
	"dst": "Distributor",
	"dtc": "Data contributor",
	"dte": "Dedicatee",
	"dtm": "Data manager",
	"dto": "Dedicator",
	"dub": "Dubious author",
	"edc": "Editor of compilation",
	"edd": "Editorial director",
	"edm": "Editor of moving image work",
	"edt": "Editor",
	"egr": "Engraver",
	"elg": "Electrician",
	"elt": "Electrotyper",
	"eng": "Engineer",
	"enj": "Enacting jurisdiction",
	"etr": "Etcher",
	"evp": "Event place",
	"exp": "Expert",
	"fac": "Facsimilist",
	"fds": "Film distributor",
	"fld": "Field director",
	"flm": "Film editor",
	"fmd": "Film director",
	"fmk": "Filmmaker",
	"fmo": "Former owner",
	"fmp": "Film producer",
	"fnd": "Funder",
	"fon": "Founder",
	"fpy": "First party",
	"frg": "Forger",
	"gdv": "Game developer",
	"gis": "Geographic information specialist",
	"his": "Host institution",
	"hnr": "Honoree",
	"hst": "Host",
	"ill": "Illustrator",
	"ilu": "Illuminator",
	"ink": "Inker",
	"ins": "Inscriber",
	"inv": "Inventor",
	"isb": "Issuing body",
	"itr": "Instrumentalist",
	"ive": "Interviewee",
	"ivr": "Interviewer",
	"jud": "Judge",
	"jug": "Jurisdiction governed",
	"lbr": "Laboratory",
	"lbt": "Librettist",
	"ldr": "Laboratory director",
	"led": "Lead",
	"lee": "Libelee-appellee",
	"lel": "Libelee",
	"len": "Lender",
	"let": "Libelee-appellant",
	"lgd": "Lighting designer",
	"lie": "Libelant-appellee",
	"lil": "Libelant",
	"lit": "Libelant-appellant",
	"lsa": "Landscape architect",
	"lse": "Licensee",
	"lso": "Licensor",
	"ltg": "Lithographer",
	"ltr": "Letterer",
	"lyr": "Lyricist",
	"mcp": "Music copyist",
	"mdc": "Metadata contact",
	"med": "Medium",
	"mfp": "Manufacture place",
	"mfr": "Manufacturer",
	"mka": "Makeup artist",
	"mod": "Moderator",
	"mon": "Monitor",
	"mrb": "Marbler",
	"mrk": "Markup editor",
	"msd": "Musical director",
	"mte": "Metal engraver",
	"mtk": "Minute taker",
	"mup": "Music programmer",
	"mus": "Musician",
	"mxe": "Mixing engineer",
	"nan": "News anchor",
	"nrt": "Narrator",
	"onp": "Onscreen participant",
	"opn": "Opponent",
	"org": "Originator",
	"orm": "Organizer",
	"osp": "Onscreen presenter",
	"oth": "Other",
	"own": "Owner",
	"pad": "Place of address",
	"pan": "Panelist",
	"pat": "Patron",
	"pbd": "Publisher director",
	"pbl": "Publisher",
	"pdr": "Project director",
	"pfr": "Proofreader",
	"pht": "Photographer",
	"plt": "Platemaker",
	"pma": "Permitting agency",
	"pmn": "Production manager",
	"pnc": "Penciller",
	"pop": "Printer of plates",
	"ppm": "Papermaker",
	"ppt": "Puppeteer",
	"pra": "Praeses",
	"prc": "Process contact",
	"prd": "Production personnel",
	"pre": "Presenter",
	"prf": "Performer",
	"prg": "Programmer",
	"prm": "Printmaker",
	"prn": "Production company",
	"pro": "Producer",
	"prp": "Production place",
	"prs": "Production designer",
	"prt": "Printer",
	"prv": "Provider",
	"pta": "Patent applicant",
	"pte": "Plaintiff-appellee",
	"ptf": "Plaintiff",
	"pth": "Patent holder",
	"ptt": "Plaintiff-appellant",
	"pup": "Publication place",
	"rap": "Rapporteur",
	"rbr": "Rubricator",
	"rcd": "Recordist",
	"rce": "Recording engineer",
	"rcp": "Addressee",
	"rdd": "Radio director",
	"red": "Redaktor",
	"ren": "Renderer",
	"res": "Researcher",
	"rev": "Reviewer",
	"rpc": "Radio producer",
	"rps": "Repository",
	"rpt": "Reporter",
	"rpy": "Responsible party",
	"rse": "Respondent-appellee",
	"rsg": "Restager",
	"rsp": "Respondent",
	"rsr": "Restorationist",
	"rst": "Respondent-appellant",
	"rth": "Research team head",
	"rtm": "Research team member",
	"rxa": "Remix artist",
	"sad": "Scientific advisor",
	"sce": "Scenarist",
	"scl": "Sculptor",
	"scr": "Scribe",
	"sde": "Sound engineer",
	"sds": "Sound designer",
	"sec": "Secretary",
	"sfx": "Special effects provider",
	"sgd": "Stage director",
	"sgn": "Signer",
	"sht": "Supporting host",
	"sll": "Seller",
	"sng": "Singer",
	"spk": "Speaker",
	"spn": "Sponsor",
	"spy": "Second party",
	"srv": "Surveyor",
	"std": "Set designer",
	"stg": "Setting",
	"stl": "Storyteller",
	"stm": "Stage manager",
	"stn": "Standards body",
	"str": "Stereotyper",
	"swd": "Software developer",
	"tad": "Technical advisor",
	"tau": "Television writer",
	"tcd": "Technical director",
	"tch": "Teacher",
	"ths": "Thesis advisor",
	"tld": "Television director",
	"tlg": "Television guest",
	"tlh": "Television host",
	"tlp": "Television producer",
	"trc": "Transcriber",
	"trl": "Translator",
	"tyd": "Type designer",
	"tyg": "Typographer",
	"uvp": "University place",
	"vac": "Voice actor",
	"vdg": "Videographer",
	"vfx": "Visual effects provider",
	"voc": "Vocalist",
	"wac": "Writer of added commentary",
	"wal": "Writer of added lyrics",
	"wam": "Writer of accompanying material",
	"wat": "Writer of added text",
	"waw": "Writer of afterword",
	"wdc": "Woodcutter",
	"wde": "Wood engraver",
	"wfs": "Writer of film story",
	"wft": "Writer of intertitles",
	"wfw": "Writer of foreword",
	"win": "Writer of introduction",
	"wit": "Witness",
	"wpr": "Writer of preface",
	"wst": "Writer of supplementary textual content",
	"wts": "Writer of television story",
}

// RelatorCodeFromURI extracts the relator code from a URI like "relators:cre"
func RelatorCodeFromURI(uri string) string {
	// Handle "relators:xxx" format
	if strings.HasPrefix(uri, "relators:") {
		return strings.TrimPrefix(uri, "relators:")
	}

	// Handle full URI like "http://id.loc.gov/vocabulary/relators/aut"
	if strings.Contains(uri, "relators/") {
		parts := strings.Split(uri, "relators/")
		if len(parts) > 1 {
			return strings.TrimSuffix(parts[1], "/")
		}
	}

	// Already just a code
	return uri
}

// RelatorLabel returns the human-readable label for a relator code.
func RelatorLabel(codeOrURI string) string {
	code := strings.ToLower(RelatorCodeFromURI(codeOrURI))

	if label, ok := MARCRelators[code]; ok {
		return label
	}

	// Return the code itself if not found
	return codeOrURI
}

// NormalizeRole normalizes a role string to a canonical form.
// Accepts MARC codes, URIs, or plain text labels.
func NormalizeRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return ""
	}

	// Extract code if it's a URI
	code := RelatorCodeFromURI(role)

	// Check if it's a known MARC code
	lowerCode := strings.ToLower(code)
	if _, ok := MARCRelators[lowerCode]; ok {
		return lowerCode
	}

	// Try to match by label
	lowerRole := strings.ToLower(role)
	for c, label := range MARCRelators {
		if strings.ToLower(label) == lowerRole {
			return c
		}
	}

	// Handle some common aliases
	aliases := map[string]string{
		"author":                 "aut",
		"authors":                "aut",
		"author of afterword":    "aft",
		"author of introduction": "aui",
		"creator":                "cre",
		"creators":               "cre",
		"editor":                 "edt",
		"editors":                "edt",
		"translator":             "trl",
		"contributor":            "ctb",
		"photographer":           "pht",
		"illustrator":            "ill",
		"advisor":                "ths",
		"thesis advisor":         "ths",
		"committee":              "dgc",
		"committee member":       "dgc",
		"publisher":              "pbl",
		"funder":                 "fnd",
		"sponsor":                "spn",
	}

	if normalized, ok := aliases[lowerRole]; ok {
		return normalized
	}

	// Return original if we can't normalize
	return role
}

// IsCreatorRole returns true if the role is a primary creator role.
func IsCreatorRole(role string) bool {
	code := NormalizeRole(role)
	creatorCodes := map[string]bool{
		"aut": true,
		"cre": true,
		"edt": true,
		"com": true,
		"trl": true,
		"ill": true,
		"pht": true,
		"art": true,
		"cmp": true,
	}
	return creatorCodes[code]
}

// RoleToCode converts a role label to its MARC relator code.
func RoleToCode(role string) string {
	return NormalizeRole(role)
}

// CodeToRole converts a MARC relator code to its label.
func CodeToRole(code string) string {
	return RelatorLabel(code)
}
