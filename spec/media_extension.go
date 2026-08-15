package spec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lehigh-university-libraries/crosswalk/model"
)

type workbenchMediaSelector struct {
	mediaType  string
	fileField  string
	extensions []string
	fallback   bool
}

// These are Workbench's default filename-to-media-bundle selectors. They are
// operational mapping policy, not Drupal labels. A sealed transformation may
// replace them when a site uses media_types or media_types_override.
var defaultWorkbenchMediaSelectors = []workbenchMediaSelector{
	{mediaType: "image", fileField: "field_media_image", extensions: []string{"png", "gif", "jpg", "jpeg"}},
	{mediaType: "document", fileField: "field_media_document", extensions: []string{"pdf", "doc", "docx", "ppt", "pptx"}},
	{mediaType: "file", fileField: "field_media_file", extensions: []string{"tif", "tiff", "jp2", "zip", "tar"}, fallback: true},
	{mediaType: "audio", fileField: "field_media_audio_file", extensions: []string{"mp3", "wav", "aac"}},
	{mediaType: "video", fileField: "field_media_video_file", extensions: []string{"mp4", "mov", "wmv", "avi", "mts", "flv", "f4v", "swf", "mkv", "webm", "ogv", "mpeg"}},
	{mediaType: "extracted_text", fileField: "field_media_file", extensions: []string{"txt"}},
}

// fabricatorMediaExtensionPolicies is the explicit compatibility preset used
// by FabricatorWorkbench. Unlike the former object-model-label switch, these
// values are serialized into and fingerprinted with the transformation.
func fabricatorMediaExtensionPolicies() []MediaExtensionPolicy {
	allowed := map[string][]string{
		"image":          {"png", "gif", "jpg", "jpeg"},
		"document":       strings.Fields("doc docx pdf ppt pptx xls xlsx"),
		"file":           strings.Fields("aux csv dat dbf doc docx fodg fodp fods fodt hocr htm html ipynb jp2 key log lyr mxd numbers odf odg odp ods odt pages pdf ppt pptx prj psd py rrd rtf sbn sbx sdw shp shx sid text tfw tif tiff txt vtt warc xls xlsx xml zip"),
		"audio":          strings.Fields("mp3 wav aac flac m4a"),
		"video":          strings.Fields("mp4 mov wmv avi mts flv f4v swf mkv webm ogv mpeg m4v dv"),
		"extracted_text": {"txt"},
	}
	return buildMediaExtensionPolicies(allowed)
}

func drupalWorkbenchMediaExtensionPolicies(snapshot *model.Snapshot) []MediaExtensionPolicy {
	allowed := make(map[string][]string)
	for _, selector := range defaultWorkbenchMediaSelectors {
		entity, exists := snapshot.Entity("media", selector.mediaType)
		if !exists {
			continue
		}
		for _, field := range entity.Fields {
			if field.Path != selector.fileField {
				continue
			}
			// Workbench writes to one configured file field per media bundle.
			// Do not infer that field from other file-valued fields: accepting
			// their extensions could pass this check and then fail in Workbench.
			if field.Kind == model.ValueFile {
				allowed[selector.mediaType] = modelFieldExtensions(field)
			}
			break
		}
	}
	return buildMediaExtensionPolicies(allowed)
}

func buildMediaExtensionPolicies(allowed map[string][]string) []MediaExtensionPolicy {
	policies := make([]MediaExtensionPolicy, 0, len(defaultWorkbenchMediaSelectors))
	for _, selector := range defaultWorkbenchMediaSelectors {
		policies = append(policies, MediaExtensionPolicy{
			MediaType:         selector.mediaType,
			SelectExtensions:  append([]string(nil), selector.extensions...),
			AllowedExtensions: append([]string(nil), allowed[selector.mediaType]...),
			Fallback:          selector.fallback,
		})
	}
	return policies
}

func modelFieldExtensions(field model.Field) []string {
	for _, settings := range []map[string]any{field.InstanceSettings, field.StorageSettings} {
		if raw, exists := settings["file_extensions"]; exists {
			return configuredExtensions(raw)
		}
	}
	return nil
}

func configuredExtensions(raw any) []string {
	var extensions []string
	switch typed := raw.(type) {
	case string:
		extensions = strings.Fields(strings.ToLower(typed))
	case []string:
		for _, extension := range typed {
			extensions = append(extensions, strings.ToLower(strings.TrimSpace(extension)))
		}
	case []any:
		for _, extension := range typed {
			extensions = append(extensions, strings.ToLower(strings.TrimSpace(fmt.Sprint(extension))))
		}
	default:
		if raw != nil {
			extensions = []string{strings.ToLower(strings.TrimSpace(fmt.Sprint(raw)))}
		}
	}
	sort.Strings(extensions)
	return compactSortedStrings(extensions)
}

func fieldMediaExtensionValidation(config drupalAttachedField) (Validation, bool) {
	if config.storage.Type != "file" && config.storage.Type != "image" && config.storage.Type != "media_track" {
		return Validation{}, false
	}
	var extensions []string
	for _, settings := range []map[string]any{config.field.Settings, config.storage.Settings} {
		if raw, exists := settings["file_extensions"]; exists {
			extensions = configuredExtensions(raw)
			break
		}
	}
	if len(extensions) == 0 {
		return Validation{}, false
	}
	return Validation{
		Rule: ValidationMediaExtension,
		MediaTypes: []MediaExtensionPolicy{{
			MediaType: "field", AllowedExtensions: extensions, Fallback: true,
		}},
	}, true
}

func configureDrupalWorkbenchMediaValidations(transformation *Transformation, snapshot *model.Snapshot) {
	policies := drupalWorkbenchMediaExtensionPolicies(snapshot)
	for index := range transformation.Source.Fields {
		field := &transformation.Source.Fields[index]
		switch field.Hub {
		case "Files.primary", "Files.supplemental", "Files.unpublished_supplemental":
			field.Validations = mergeDrupalFieldValidations(field.Validations, []Validation{{
				Rule: ValidationMediaExtension, MediaTypes: cloneMediaExtensionPolicies(policies),
			}})
		}
	}
}

func cloneMediaExtensionPolicies(input []MediaExtensionPolicy) []MediaExtensionPolicy {
	result := make([]MediaExtensionPolicy, len(input))
	for index, policy := range input {
		result[index] = policy
		result[index].SelectExtensions = append([]string(nil), policy.SelectExtensions...)
		result[index].AllowedExtensions = append([]string(nil), policy.AllowedExtensions...)
	}
	return result
}
