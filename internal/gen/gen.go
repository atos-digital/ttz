package gen

import (
	"embed"
	"encoding/json"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/atos-digital/ttz/internal/vscode"
	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/ast/astutil"
)

type Parser struct {
	// DirPath is the path to the folder to parse.
	// This should be a directory containing the go files to parse.
	DirPath        string
	CurrentModName string
	NewModName     string
	Template       embed.FS
}

func NewParser(dirPath, defaultModuleName, newModuleName string, template embed.FS) *Parser {
	return &Parser{
		DirPath:        dirPath,
		CurrentModName: defaultModuleName,
		NewModName:     newModuleName,
		Template:       template,
	}
}

func (p Parser) copyFile(path string, src fs.File) error {
	dst, err := os.Create(filepath.Join(p.DirPath, path))
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

func (p *Parser) Parse() error {
	return fs.WalkDir(p.Template, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			err := os.MkdirAll(filepath.Join(p.DirPath, path), 0777)
			if err != nil {
				return err
			}
		} else {
			src, err := p.Template.Open(path)
			if err != nil {
				return err
			}
			defer src.Close()

			switch {
			case strings.HasSuffix(path, ".mod"):
				err := p.updateMod(path, src, p.NewModName)
				if err != nil {
					return err
				}
			case path == "folder.go":
				// skip folder.go
			case strings.HasSuffix(path, ".go"):
				err := p.updateFile(path, src, p.CurrentModName, p.NewModName)
				if err != nil {
					return err
				}
			case path == ".vscode/settings.json":
				err := p.updateVscodeSettings(path, src)
				if err != nil {
					return err
				}
			default:
				err = p.copyFile(path, src)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (p *Parser) updateMod(path string, src fs.File, modName string) error {
	bytes, err := io.ReadAll(src)
	if err != nil {
		return err
	}

	file, err := modfile.Parse(path, bytes, nil)
	if err != nil {
		return err
	}
	file.AddModuleStmt(modName)

	newBytes := modfile.Format(file.Syntax)
	return os.WriteFile(filepath.Join(p.DirPath, path), newBytes, 0644)
}

func (p *Parser) updateFile(path string, src fs.File, oldModName, newModName string) error {
	fset := token.NewFileSet()
	bytes, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	file, err := parser.ParseFile(fset, "", bytes, parser.ParseComments)
	if err != nil {
		return err
	}

	imports := astutil.Imports(fset, file)
	for _, para := range imports {
		for _, imp := range para {
			oldPath, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			if strings.Contains(oldPath, oldModName) {
				newPath := strings.Replace(oldPath, oldModName, newModName, 1)
				rewritten := astutil.RewriteImport(fset, file, oldPath, newPath)
				if !rewritten {
					return err
				}
			}
		}
	}

	dst, err := os.Create(filepath.Join(p.DirPath, path))
	if err != nil {
		return err
	}
	defer dst.Close()

	return format.Node(dst, fset, file)
}

func (p *Parser) updateVscodeSettings(path string, src fs.File) error {
	set := vscode.Settings{}
	err := json.NewDecoder(src).Decode(&set)
	if err != nil {
		return err
	}
	set.SetGopls(vscode.Gopls{
		FormattingLocal:   p.NewModName,
		FormattingGofumpt: true,
	})
	dst, err := os.Create(filepath.Join(p.DirPath, path))
	if err != nil {
		return err
	}
	defer dst.Close()
	enc := json.NewEncoder(dst)
	enc.SetIndent("", "  ")
	return enc.Encode(set)
}
