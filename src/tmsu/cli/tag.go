/*
Copyright 2011-2015 Paul Ruane.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tmsu/common/fingerprint"
	"tmsu/common/log"
	"tmsu/entities"
	"tmsu/storage"
)

var TagCommand = Command{
	Name:     "tag",
	Synopsis: "Apply tags to files",
	Usages: []string{"tmsu tag [OPTION]... FILE TAG[=VALUE]...",
		`tmsu tag [OPTION]... --tags="TAG[=VALUE]..." FILE...`,
		"tmsu tag [OPTION]... --from=SOURCE FILE...",
		"tmsu tag [OPTION]... --create TAG[=VALUE]..."},
	Description: `Tags the file FILE with the TAGs specified. If no TAG is specified then all tags are listed.

Tag names may consist of one or more letter, number, punctuation and symbol characters (from the corresponding Unicode categories). Tag names may not contain whitespace characters, the comparison operator symbols ('=', '<' and '>"), parentheses ('(' and ')'), commas (',') or the slash symbol ('/'). In addition, the tag names '.' and '..' are not valid.

Optionally tags applied to files may be attributed with a VALUE using the TAG=VALUE syntax.`,
	Examples: []string{"$ tmsu tag mountain1.jpg photo landscape holiday good country=france",
		"$ tmsu tag --from=mountain1.jpg mountain2.jpg",
		`$ tmsu tag --tags="landscape" field1.jpg field2.jpg`,
		"$ tmsu tag --create bad rubbish awful"},
	Options: Options{{"--tags", "-t", "the set of tags to apply", true, ""},
		{"--recursive", "-r", "recursively apply tags to directory contents", false, ""},
		{"--from", "-f", "copy tags from the SOURCE file", true, ""},
		{"--create", "-c", "create tags without tagging any files", false, ""},
		{"--explicit", "-e", "explicitly apply tags even if they are already implied", false, ""}},
	Exec: tagExec,
}

func tagExec(store *storage.Storage, options Options, args []string) error {
	recursive := options.HasOption("--recursive")
	explicit := options.HasOption("--explicit")

	switch {
	case options.HasOption("--create"):
		if len(args) == 0 {
			return fmt.Errorf("set of tags to create must be specified")
		}

		if err := createTags(store, args); err != nil {
			return err
		}
	case options.HasOption("--tags"):
		if len(args) < 1 {
			return fmt.Errorf("files to tag must be specified")
		}

		tagArgs := strings.Fields(options.Get("--tags").Argument)
		if len(tagArgs) == 0 {
			return fmt.Errorf("set of tags to apply must be specified")
		}

		paths := args
		if len(paths) < 1 {
			return fmt.Errorf("at least one file to tag must be specified")
		}

		if err := tagPaths(store, tagArgs, paths, explicit, recursive); err != nil {
			return err
		}
	case options.HasOption("--from"):
		if len(args) < 1 {
			return fmt.Errorf("files to tag must be specified")
		}

		fromPath, err := filepath.Abs(options.Get("--from").Argument)
		if err != nil {
			return fmt.Errorf("%v: could not get absolute path: %v", fromPath, err)
		}

		paths := args

		if err := tagFrom(store, fromPath, paths, explicit, recursive); err != nil {
			return err
		}
	default:
		if len(args) < 2 {
			return fmt.Errorf("file to tag and tags to apply must be specified")
		}

		paths := args[0:1]
		tagArgs := args[1:]

		if err := tagPaths(store, tagArgs, paths, explicit, recursive); err != nil {
			return err
		}
	}

	return nil
}

func createTags(store *storage.Storage, tagNames []string) error {
	wereErrors := false
	for _, tagName := range tagNames {
		tag, err := store.TagByName(tagName)
		if err != nil {
			return fmt.Errorf("could not check if tag '%v' exists: %v", tagName, err)
		}

		if tag == nil {
			log.Infof(2, "adding tag '%v'.", tagName)

			_, err := store.AddTag(tagName)
			if err != nil {
				return fmt.Errorf("could not add tag '%v': %v", tagName, err)
			}
		} else {
			log.Warnf("tag '%v' already exists", tagName)
			wereErrors = true
		}
	}

	if wereErrors {
		return errBlank
	}

	return nil
}

func tagPaths(store *storage.Storage, tagArgs, paths []string, explicit, recursive bool) error {
	fingerprintAlgorithm, err := store.SettingAsString("fingerprintAlgorithm")
	if err != nil {
		return err
	}

	autoCreateTags, err := store.SettingAsBool("autoCreateTags")
	if err != nil {
		return err
	}

	autoCreateValues, err := store.SettingAsBool("autoCreateValues")
	if err != nil {
		return err
	}

	wereErrors := false
	tagValuePairs := make([]TagValuePair, 0, 10)
	for _, tagArg := range tagArgs {
		var tagName, valueName string
		index := strings.Index(tagArg, "=")

		switch index {
		case -1, 0:
			tagName = tagArg
		default:
			tagName = tagArg[0:index]
			valueName = tagArg[index+1 : len(tagArg)]
		}

		tag, err := getTag(store, tagName)
		if err != nil {
			return err
		}
		if tag == nil {
			if autoCreateTags {
				tag, err = createTag(store, tagName)
				if err != nil {
					return err
				}
			} else {
				log.Warnf("no such tag '%v'.", tagName)
				wereErrors = true
				continue
			}
		}

		value, err := getValue(store, valueName)
		if err != nil {
			return err
		}
		if value == nil {
			if autoCreateValues {
				value, err = createValue(store, valueName)
				if err != nil {
					return err
				}
			} else {
				log.Warnf("no such value '%v'.", valueName)
				wereErrors = true
				continue
			}
		}

		tagValuePairs = append(tagValuePairs, TagValuePair{tag.Id, value.Id})
	}

	for _, path := range paths {
		if err := tagPath(store, path, tagValuePairs, explicit, recursive, fingerprintAlgorithm); err != nil {
			switch {
			case os.IsPermission(err):
				log.Warnf("%v: permisison denied", path)
				wereErrors = true
			case os.IsNotExist(err):
				log.Warnf("%v: no such file", path)
				wereErrors = true
			default:
				return fmt.Errorf("%v: could not stat file: %v", path, err)
			}
		}
	}

	if wereErrors {
		return errBlank
	}

	return nil
}

func tagFrom(store *storage.Storage, fromPath string, paths []string, explicit, recursive bool) error {
	fingerprintAlgorithmSetting, err := store.Setting("fingerprintAlgorithm")
	if err != nil {
		return fmt.Errorf("could not retrieve fingerprint algorithm: %v", err)
	}

	file, err := store.FileByPath(fromPath)
	if err != nil {
		return fmt.Errorf("%v: could not retrieve file: %v", fromPath, err)
	}
	if file == nil {
		return fmt.Errorf("%v: path is not tagged")
	}

	fileTags, err := store.FileTagsByFileId(file.Id, true)
	if err != nil {
		return fmt.Errorf("%v: could not retrieve filetags: %v", fromPath, err)
	}

	tagValuePairs := make([]TagValuePair, len(fileTags))
	for index, fileTag := range fileTags {
		tagValuePairs[index] = TagValuePair{fileTag.TagId, fileTag.ValueId}
	}

	wereErrors := false
	for _, path := range paths {
		if err := tagPath(store, path, tagValuePairs, explicit, recursive, fingerprintAlgorithmSetting.Value); err != nil {
			switch {
			case os.IsPermission(err):
				log.Warnf("%v: permisison denied", path)
				wereErrors = true
			case os.IsNotExist(err):
				log.Warnf("%v: no such file", path)
				wereErrors = true
			default:
				return fmt.Errorf("%v: could not stat file: %v", path, err)
			}
		}
	}

	if wereErrors {
		return errBlank
	}

	return nil
}

func tagPath(store *storage.Storage, path string, tagValuePairs []TagValuePair, explicit, recursive bool, fingerprintAlgorithm string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%v: could not get absolute path: %v", path, err)
	}

	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			stat, err = os.Lstat(path)
			if err != nil {
				return err
			}

			log.Warnf("%v: tagging broken symbolic link", path)
		} else {
			return err
		}
	}

	log.Infof(2, "%v: checking if file exists", path)

	file, err := store.FileByPath(absPath)
	if err != nil {
		return fmt.Errorf("%v: could not retrieve file: %v", path, err)
	}
	if file == nil {
		file, err = addFile(store, absPath, stat.ModTime(), uint(stat.Size()), stat.IsDir(), fingerprintAlgorithm)
		if err != nil {
			return fmt.Errorf("%v: could not add file: %v", path, err)
		}
	}

	if !explicit {
		tagValuePairs, err = removeAlreadyAppliedTagValuePairs(store, tagValuePairs, file)
		if err != nil {
			return fmt.Errorf("%v: could not remove applied tags: %v", path, err)
		}
	}

	log.Infof(2, "%v: applying tags.", path)

	for _, tagValuePair := range tagValuePairs {
		if _, err = store.AddFileTag(file.Id, tagValuePair.TagId, tagValuePair.ValueId); err != nil {
			return fmt.Errorf("%v: could not apply tags: %v", file.Path(), err)
		}
	}

	if recursive && stat.IsDir() {
		if err = tagRecursively(store, path, tagValuePairs, explicit, fingerprintAlgorithm); err != nil {
			return err
		}
	}

	return nil
}

func tagRecursively(store *storage.Storage, path string, tagValuePairs []TagValuePair, explicit bool, fingerprintAlgorithm string) error {
	osFile, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%v: could not open path: %v", path, err)
	}

	childNames, err := osFile.Readdirnames(0)
	osFile.Close()
	if err != nil {
		return fmt.Errorf("%v: could not retrieve directory contents: %v", path, err)
	}

	for _, childName := range childNames {
		childPath := filepath.Join(path, childName)

		if err = tagPath(store, childPath, tagValuePairs, explicit, true, fingerprintAlgorithm); err != nil {
			return err
		}
	}

	return nil
}

func getTag(store *storage.Storage, tagName string) (*entities.Tag, error) {
	tag, err := store.TagByName(tagName)
	if err != nil {
		return nil, fmt.Errorf("could not look up tag '%v': %v", tagName, err)
	}

	return tag, nil
}

func createTag(store *storage.Storage, tagName string) (*entities.Tag, error) {
	tag, err := store.AddTag(tagName)
	if err != nil {
		return nil, fmt.Errorf("could not create tag '%v': %v", tagName, err)
	}

	log.Warnf("New tag '%v'.", tagName)

	return tag, nil
}

func getValue(store *storage.Storage, valueName string) (*entities.Value, error) {
	value, err := store.ValueByName(valueName)
	if err != nil {
		return nil, fmt.Errorf("could not look up value '%v': %v", valueName, err)
	}

	return value, nil
}

func createValue(store *storage.Storage, valueName string) (*entities.Value, error) {
	value, err := store.AddValue(valueName)
	if err != nil {
		return nil, err
	}

	log.Warnf("New value '%v'.", valueName)

	return value, nil
}

func addFile(store *storage.Storage, path string, modTime time.Time, size uint, isDir bool, fingerprintAlgorithm string) (*entities.File, error) {
	log.Infof(2, "%v: creating fingerprint", path)

	fingerprint, err := fingerprint.Create(path, fingerprintAlgorithm)
	if err != nil {
		return nil, fmt.Errorf("%v: could not create fingerprint: %v", path, err)
	}

	log.Infof(2, "%v: adding file.", path)

	file, err := store.AddFile(path, fingerprint, modTime, int64(size), isDir)
	if err != nil {
		return nil, fmt.Errorf("%v: could not add file to database: %v", path, err)
	}

	return file, nil
}

func removeAlreadyAppliedTagValuePairs(store *storage.Storage, tagValuePairs []TagValuePair, file *entities.File) ([]TagValuePair, error) {
	log.Infof(2, "%v: determining existing file-tags", file.Path())

	existingFileTags, err := store.FileTagsByFileId(file.Id, false)
	if err != nil {
		return nil, fmt.Errorf("%v: could not determine file's tags: %v", file.Path(), err)
	}

	log.Infof(2, "%v: determining implied tags", file.Path())

	tagIds := make(entities.TagIds, len(tagValuePairs))
	for index, tagValuePair := range tagValuePairs {
		tagIds[index] = tagValuePair.TagId
	}

	newlyImpliedTags, err := store.ImplicationsForTags(tagIds...)
	if err != nil {
		return nil, fmt.Errorf("%v: could not determine implied tags: %v", file.Path(), err)
	}

	log.Infof(2, "%v: revising set of tags to apply", file.Path())

	revisedTagValuePairs := make([]TagValuePair, 0, len(tagValuePairs))
	for _, tagValuePair := range tagValuePairs {
		if existingFileTags.Contains(tagValuePair.TagId, tagValuePair.ValueId) {
			continue
		}

		if tagValuePair.ValueId == 0 && newlyImpliedTags.Implies(tagValuePair.TagId) {
			continue
		}

		revisedTagValuePairs = append(revisedTagValuePairs, tagValuePair)
	}

	return revisedTagValuePairs, nil
}
