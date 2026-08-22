package docker

import "strings"

// This file carries a verbatim copy of the two word lists Docker's daemon
// draws on when it names a container you did not name yourself
// (moby/moby, pkg/namesgenerator): a lowercase adjective, an underscore,
// and the surname of a scientist or engineer — "nostalgic_ptolemy",
// "vibrant_curie".
//
// Copying the lists is the only way to tell those apart from a name the
// user chose. A bare pattern like "two lowercase words joined by an
// underscore" would flag a hand-named "my_db" or "api_cache" as throwaway
// junk, and this package's whole job is deciding what is safe to throw
// away. Membership in both lists is proof the daemon invented the name,
// which in turn means nobody cared enough about the container to name it.
//
// The lists only ever grow upstream, so a stale copy fails in the safe
// direction: a newly added word means we miss a generated name and call
// the container merely worth reviewing instead of disposable. We never
// mistake a deliberately named container for junk.

// generatedAdjectives is namesgenerator's "left" list.
var generatedAdjectives = map[string]struct{}{
	"admiring": {}, "adoring": {}, "affectionate": {}, "agitated": {}, "amazing": {}, "angry": {},
	"awesome": {}, "beautiful": {}, "blissful": {}, "bold": {}, "boring": {}, "brave": {},
	"busy": {}, "charming": {}, "clever": {}, "compassionate": {}, "competent": {}, "condescending": {},
	"confident": {}, "cool": {}, "cranky": {}, "crazy": {}, "dazzling": {}, "determined": {},
	"distracted": {}, "dreamy": {}, "eager": {}, "ecstatic": {}, "elastic": {}, "elated": {},
	"elegant": {}, "eloquent": {}, "epic": {}, "exciting": {}, "fervent": {}, "festive": {},
	"flamboyant": {}, "focused": {}, "friendly": {}, "frosty": {}, "funny": {}, "gallant": {},
	"gifted": {}, "goofy": {}, "gracious": {}, "great": {}, "happy": {}, "hardcore": {},
	"heuristic": {}, "hopeful": {}, "hungry": {}, "infallible": {}, "inspiring": {}, "intelligent": {},
	"interesting": {}, "jolly": {}, "jovial": {}, "keen": {}, "kind": {}, "laughing": {},
	"loving": {}, "lucid": {}, "magical": {}, "modest": {}, "musing": {}, "mystifying": {},
	"naughty": {}, "nervous": {}, "nice": {}, "nifty": {}, "nostalgic": {}, "objective": {},
	"optimistic": {}, "peaceful": {}, "pedantic": {}, "pensive": {}, "practical": {}, "priceless": {},
	"quirky": {}, "quizzical": {}, "recursing": {}, "relaxed": {}, "reverent": {}, "romantic": {},
	"sad": {}, "serene": {}, "sharp": {}, "silly": {}, "sleepy": {}, "stoic": {},
	"strange": {}, "stupefied": {}, "suspicious": {}, "sweet": {}, "tender": {}, "thirsty": {},
	"trusting": {}, "unruffled": {}, "upbeat": {}, "vibrant": {}, "vigilant": {}, "vigorous": {},
	"wizardly": {}, "wonderful": {}, "xenodochial": {}, "youthful": {}, "zealous": {}, "zen": {},
}

// generatedSurnames is namesgenerator's "right" list.
var generatedSurnames = map[string]struct{}{
	"agnesi": {}, "albattani": {}, "allen": {}, "almeida": {}, "antonelli": {}, "archimedes": {},
	"ardinghelli": {}, "aryabhata": {}, "austin": {}, "babbage": {}, "banach": {}, "banzai": {},
	"bardeen": {}, "bartik": {}, "bassi": {}, "beaver": {}, "bell": {}, "benz": {},
	"bhabha": {}, "bhaskara": {}, "black": {}, "blackburn": {}, "blackwell": {}, "bohr": {},
	"booth": {}, "borg": {}, "bose": {}, "bouman": {}, "boyd": {}, "brahmagupta": {},
	"brattain": {}, "brown": {}, "buck": {}, "burnell": {}, "cannon": {}, "carson": {},
	"cartwright": {}, "carver": {}, "cerf": {}, "chandrasekhar": {}, "chaplygin": {}, "chatelet": {},
	"chatterjee": {}, "chaum": {}, "chebyshev": {}, "clarke": {}, "cohen": {}, "colden": {},
	"cori": {}, "cray": {}, "curie": {}, "curran": {}, "darwin": {}, "davinci": {},
	"dewdney": {}, "dhawan": {}, "diffie": {}, "dijkstra": {}, "dirac": {}, "driscoll": {},
	"dubinsky": {}, "easley": {}, "edison": {}, "einstein": {}, "elbakyan": {}, "elgamal": {},
	"elion": {}, "ellis": {}, "engelbart": {}, "euclid": {}, "euler": {}, "faraday": {},
	"feistel": {}, "fermat": {}, "fermi": {}, "feynman": {}, "franklin": {}, "gagarin": {},
	"galileo": {}, "galois": {}, "ganguly": {}, "gates": {}, "gauss": {}, "germain": {},
	"goldberg": {}, "goldstine": {}, "goldwasser": {}, "golick": {}, "goodall": {}, "gould": {},
	"greider": {}, "grothendieck": {}, "haibt": {}, "hamilton": {}, "haslett": {}, "hawking": {},
	"heisenberg": {}, "hellman": {}, "hermann": {}, "herschel": {}, "hertz": {}, "heyrovsky": {},
	"hodgkin": {}, "hofstadter": {}, "hoover": {}, "hopper": {}, "hugle": {}, "hypatia": {},
	"ishizaka": {}, "jackson": {}, "jang": {}, "jemison": {}, "jennings": {}, "jepsen": {},
	"johnson": {}, "joliot": {}, "jones": {}, "kalam": {}, "kapitsa": {}, "kare": {},
	"keldysh": {}, "keller": {}, "kepler": {}, "khayyam": {}, "khorana": {}, "kilby": {},
	"kirch": {}, "knuth": {}, "kowalevski": {}, "lalande": {}, "lamarr": {}, "lamport": {},
	"leakey": {}, "leavitt": {}, "lederberg": {}, "lehmann": {}, "lewin": {}, "lichterman": {},
	"liskov": {}, "lovelace": {}, "lumiere": {}, "mahavira": {}, "margulis": {}, "matsumoto": {},
	"maxwell": {}, "mayer": {}, "mccarthy": {}, "mcclintock": {}, "mclaren": {}, "mclean": {},
	"mcnulty": {}, "meitner": {}, "mendel": {}, "mendeleev": {}, "meninsky": {}, "merkle": {},
	"mestorf": {}, "mirzakhani": {}, "montalcini": {}, "moore": {}, "morse": {}, "moser": {},
	"murdock": {}, "napier": {}, "nash": {}, "neumann": {}, "newton": {}, "nightingale": {},
	"nobel": {}, "noether": {}, "northcutt": {}, "noyce": {}, "panini": {}, "pare": {},
	"pascal": {}, "pasteur": {}, "payne": {}, "perlman": {}, "pike": {}, "poincare": {},
	"poitras": {}, "proskuriakova": {}, "ptolemy": {}, "raman": {}, "ramanujan": {}, "rhodes": {},
	"ride": {}, "ritchie": {}, "robinson": {}, "roentgen": {}, "rosalind": {}, "rubin": {},
	"saha": {}, "sammet": {}, "sanderson": {}, "satoshi": {}, "shamir": {}, "shannon": {},
	"shaw": {}, "shirley": {}, "shockley": {}, "shtern": {}, "sinoussi": {}, "snyder": {},
	"solomon": {}, "spence": {}, "stonebraker": {}, "sutherland": {}, "swanson": {}, "swartz": {},
	"swirles": {}, "taussig": {}, "tesla": {}, "tharp": {}, "thompson": {}, "torvalds": {},
	"tu": {}, "turing": {}, "varahamihira": {}, "vaughan": {}, "villani": {}, "visvesvaraya": {},
	"volhard": {}, "wescoff": {}, "wilbur": {}, "wiles": {}, "williams": {}, "williamson": {},
	"wilson": {}, "wing": {}, "wozniak": {}, "wright": {}, "wu": {}, "yalow": {},
	"yonath": {}, "zhukovsky": {},
}

// isGeneratedName reports whether name looks like one the Docker daemon
// invented rather than one a human typed.
//
// Two details of the real generator are worth handling. Names arrive from
// docker inspect with a leading slash, and when the daemon's first pick is
// already taken it retries with a single digit appended
// ("nostalgic_ptolemy3"), so one trailing digit is stripped before the
// lookup. Compose's own default names are not generated names in this
// sense: they are "project-service-1" or "project_service_1", which split
// into three parts and so never match.
func isGeneratedName(name string) bool {
	name = strings.TrimPrefix(name, "/")
	if name == "" {
		return false
	}
	if last := name[len(name)-1]; last >= '0' && last <= '9' {
		name = name[:len(name)-1]
	}
	adj, surname, ok := strings.Cut(name, "_")
	if !ok {
		return false
	}
	if _, ok := generatedAdjectives[adj]; !ok {
		return false
	}
	_, ok = generatedSurnames[surname]
	return ok
}
