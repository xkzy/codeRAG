package reverse

type NormalizedFunction struct {
	Address          string
	Name             string
	Size             *int
	Calls            []string
	Strings          []string
	DecompilerOutput string
}

type NormalizedBinary struct {
	BinaryID  string
	Path      string
	Sha256    string
	Tool      string
	Functions []NormalizedFunction
}
