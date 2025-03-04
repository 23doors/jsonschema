package jsonschema

import (
	"strconv"
	"strings"
)

// A Draft represents json-schema draft
type Draft struct {
	version    int
	meta       *Schema
	id         string // property name used to represent schema id.
	boolSchema bool   // is boolean valid schema
	subschemas map[string]position
}

func (d *Draft) loadMeta(base string, schemas map[string]string) {
	c := NewCompiler()
	c.AssertFormat = true
	for u, schema := range schemas {
		if err := c.AddResource(base+"/"+u, strings.NewReader(schema)); err != nil {
			panic(err)
		}
	}
	d.meta = c.MustCompile(base + "/schema")
}

func (d *Draft) getID(sch interface{}) string {
	m, ok := sch.(*OrderedMap)
	if !ok {
		return ""
	}
	if _, ok := m.Get("$ref"); ok && d.version <= 7 {
		// $ref prevents a sibling id from changing the base uri
		return ""
	}
	v, ok := m.Get(d.id)
	if !ok {
		return ""
	}
	id, ok := v.(string)
	if !ok {
		return ""
	}
	return id
}

func (d *Draft) resolveID(base string, sch interface{}) (string, error) {
	id, _ := split(d.getID(sch)) // strip fragment
	if id == "" {
		return "", nil
	}
	url, err := resolveURL(base, id)
	url, _ = split(url) // strip fragment
	return url, err
}

func (d *Draft) anchors(sch interface{}) []string {
	m, ok := sch.(*OrderedMap)
	if !ok {
		return nil
	}

	var anchors []string

	// before draft2019, anchor is specified in id
	_, f := split(d.getID(m))
	if f != "#" {
		anchors = append(anchors, f[1:])
	}

	if v, ok := m.Get("$anchor"); ok && d.version >= 2019 {
		anchors = append(anchors, v.(string))
	}
	if v, ok := m.Get("$dynamicAnchor"); ok && d.version >= 2020 {
		anchors = append(anchors, v.(string))
	}
	return anchors
}

// listSubschemas collects subschemas in r into rr.
func (d *Draft) listSubschemas(r *resource, base string, rr map[string]*resource) error {
	add := func(loc string, sch interface{}) error {
		url, err := d.resolveID(base, sch)
		if err != nil {
			return err
		}
		floc := r.floc + "/" + loc
		sr := &resource{url: url, floc: floc, doc: sch}
		rr[floc] = sr

		base := base
		if url != "" {
			base = url
		}
		return d.listSubschemas(sr, base, rr)
	}

	sch, ok := r.doc.(*OrderedMap)
	if !ok {
		return nil
	}
	for kw, pos := range d.subschemas {
		v, ok := sch.Get(kw)
		if !ok {
			continue
		}
		if pos&self != 0 {
			switch v := v.(type) {
			case *OrderedMap:
				if err := add(kw, v); err != nil {
					return err
				}
			case bool:
				if d.boolSchema {
					if err := add(kw, v); err != nil {
						return err
					}
				}
			}
		}
		if pos&item != 0 {
			if v, ok := v.([]interface{}); ok {
				for i, item := range v {
					if err := add(kw+"/"+strconv.Itoa(i), item); err != nil {
						return err
					}
				}
			}
		}
		if pos&prop != 0 {
			if v, ok := v.(*OrderedMap); ok {
				for pname, pval := range v.RawValues() {
					if err := add(kw+"/"+escape(pname), pval); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

type position uint

const (
	self position = 1 << iota
	prop
	item
)

// supported drafts
var (
	Draft4    = &Draft{version: 4, id: "id", boolSchema: false}
	Draft6    = &Draft{version: 6, id: "$id", boolSchema: true}
	Draft7    = &Draft{version: 7, id: "$id", boolSchema: true}
	Draft2019 = &Draft{version: 2019, id: "$id", boolSchema: true}
	Draft2020 = &Draft{version: 2020, id: "$id", boolSchema: true}

	latest = Draft2020
)

func findDraft(url string) *Draft {
	if strings.HasPrefix(url, "http://") {
		url = "https://" + strings.TrimPrefix(url, "http://")
	}
	if strings.HasSuffix(url, "#") || strings.HasSuffix(url, "#/") {
		url = url[:strings.IndexByte(url, '#')]
	}
	switch url {
	case "https://json-schema.org/schema":
		return latest
	case "https://json-schema.org/draft/2020-12/schema":
		return Draft2020
	case "https://json-schema.org/draft/2019-09/schema":
		return Draft2019
	case "https://json-schema.org/draft-07/schema":
		return Draft7
	case "https://json-schema.org/draft-06/schema":
		return Draft6
	case "https://json-schema.org/draft-04/schema":
		return Draft4
	}
	return nil
}

func init() {
	subschemas := map[string]position{
		// type agnostic
		"definitions": prop,
		"not":         self,
		"allOf":       item,
		"anyOf":       item,
		"oneOf":       item,
		// object
		"properties":           prop,
		"additionalProperties": self,
		"patternProperties":    prop,
		// array
		"items":           self | item,
		"additionalItems": self,
		"dependencies":    prop,
	}
	Draft4.subschemas = clone(subschemas)

	subschemas["propertyNames"] = self
	subschemas["contains"] = self
	Draft6.subschemas = clone(subschemas)

	subschemas["if"] = self
	subschemas["then"] = self
	subschemas["else"] = self
	Draft7.subschemas = clone(subschemas)

	subschemas["$defs"] = prop
	subschemas["dependentSchemas"] = prop
	subschemas["unevaluatedProperties"] = self
	subschemas["unevaluatedItems"] = self
	Draft2019.subschemas = clone(subschemas)

	subschemas["prefixItems"] = item
	Draft2020.subschemas = clone(subschemas)

	Draft4.loadMeta("http://json-schema.org/draft-04", map[string]string{
		"schema": `{
			"$schema": "http://json-schema.org/draft-04/schema#",
			"description": "Core schema meta-schema",
			"definitions": {
				"schemaArray": {
					"type": "array",
					"minItems": 1,
					"items": { "$ref": "#" }
				},
				"positiveInteger": {
					"type": "integer",
					"minimum": 0
				},
				"positiveIntegerDefault0": {
					"allOf": [ { "$ref": "#/definitions/positiveInteger" }, { "default": 0 } ]
				},
				"simpleTypes": {
					"enum": [ "array", "boolean", "integer", "null", "number", "object", "string" ]
				},
				"stringArray": {
					"type": "array",
					"items": { "type": "string" },
					"minItems": 1,
					"uniqueItems": true
				}
			},
			"type": "object",
			"properties": {
				"id": {
					"type": "string",
					"format": "uriref"
				},
				"$schema": {
					"type": "string",
					"format": "uri"
				},
				"title": {
					"type": "string"
				},
				"description": {
					"type": "string"
				},
				"default": {},
				"multipleOf": {
					"type": "number",
					"minimum": 0,
					"exclusiveMinimum": true
				},
				"maximum": {
					"type": "number"
				},
				"exclusiveMaximum": {
					"type": "boolean",
					"default": false
				},
				"minimum": {
					"type": "number"
				},
				"exclusiveMinimum": {
					"type": "boolean",
					"default": false
				},
				"maxLength": { "$ref": "#/definitions/positiveInteger" },
				"minLength": { "$ref": "#/definitions/positiveIntegerDefault0" },
				"pattern": {
					"type": "string",
					"format": "regex"
				},
				"additionalItems": {
					"anyOf": [
						{ "type": "boolean" },
						{ "$ref": "#" }
					],
					"default": {}
				},
				"items": {
					"anyOf": [
						{ "$ref": "#" },
						{ "$ref": "#/definitions/schemaArray" }
					],
					"default": {}
				},
				"maxItems": { "$ref": "#/definitions/positiveInteger" },
				"minItems": { "$ref": "#/definitions/positiveIntegerDefault0" },
				"uniqueItems": {
					"type": "boolean",
					"default": false
				},
				"maxProperties": { "$ref": "#/definitions/positiveInteger" },
				"minProperties": { "$ref": "#/definitions/positiveIntegerDefault0" },
				"required": { "$ref": "#/definitions/stringArray" },
				"additionalProperties": {
					"anyOf": [
						{ "type": "boolean" },
						{ "$ref": "#" }
					],
					"default": {}
				},
				"definitions": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"properties": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"patternProperties": {
					"type": "object",
					"regexProperties": true,
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"regexProperties": { "type": "boolean" },
				"dependencies": {
					"type": "object",
					"additionalProperties": {
						"anyOf": [
							{ "$ref": "#" },
							{ "$ref": "#/definitions/stringArray" }
						]
					}
				},
				"enum": {
					"type": "array",
					"minItems": 1,
					"uniqueItems": true
				},
				"type": {
					"anyOf": [
						{ "$ref": "#/definitions/simpleTypes" },
						{
							"type": "array",
							"items": { "$ref": "#/definitions/simpleTypes" },
							"minItems": 1,
							"uniqueItems": true
						}
					]
				},
				"allOf": { "$ref": "#/definitions/schemaArray" },
				"anyOf": { "$ref": "#/definitions/schemaArray" },
				"oneOf": { "$ref": "#/definitions/schemaArray" },
				"not": { "$ref": "#" },
				"format": { "type": "string" },
				"$ref": { "type": "string" }
			},
			"dependencies": {
				"exclusiveMaximum": [ "maximum" ],
				"exclusiveMinimum": [ "minimum" ]
			},
			"default": {}
		}`,
	})
	Draft6.loadMeta("http://json-schema.org/draft-06", map[string]string{
		"schema": `{
			"$schema": "http://json-schema.org/draft-06/schema#",
			"$id": "http://json-schema.org/draft-06/schema#",
			"title": "Core schema meta-schema",
			"definitions": {
				"schemaArray": {
					"type": "array",
					"minItems": 1,
					"items": { "$ref": "#" }
				},
				"nonNegativeInteger": {
					"type": "integer",
					"minimum": 0
				},
				"nonNegativeIntegerDefault0": {
					"allOf": [
						{ "$ref": "#/definitions/nonNegativeInteger" },
						{ "default": 0 }
					]
				},
				"simpleTypes": {
					"enum": [
						"array",
						"boolean",
						"integer",
						"null",
						"number",
						"object",
						"string"
					]
				},
				"stringArray": {
					"type": "array",
					"items": { "type": "string" },
					"uniqueItems": true,
					"default": []
				}
			},
			"type": ["object", "boolean"],
			"properties": {
				"$id": {
					"type": "string",
					"format": "uri-reference"
				},
				"$schema": {
					"type": "string",
					"format": "uri"
				},
				"$ref": {
					"type": "string",
					"format": "uri-reference"
				},
				"title": {
					"type": "string"
				},
				"description": {
					"type": "string"
				},
				"default": {},
				"multipleOf": {
					"type": "number",
					"exclusiveMinimum": 0
				},
				"maximum": {
					"type": "number"
				},
				"exclusiveMaximum": {
					"type": "number"
				},
				"minimum": {
					"type": "number"
				},
				"exclusiveMinimum": {
					"type": "number"
				},
				"maxLength": { "$ref": "#/definitions/nonNegativeInteger" },
				"minLength": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"pattern": {
					"type": "string",
					"format": "regex"
				},
				"additionalItems": { "$ref": "#" },
				"items": {
					"anyOf": [
						{ "$ref": "#" },
						{ "$ref": "#/definitions/schemaArray" }
					],
					"default": {}
				},
				"maxItems": { "$ref": "#/definitions/nonNegativeInteger" },
				"minItems": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"uniqueItems": {
					"type": "boolean",
					"default": false
				},
				"contains": { "$ref": "#" },
				"maxProperties": { "$ref": "#/definitions/nonNegativeInteger" },
				"minProperties": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"required": { "$ref": "#/definitions/stringArray" },
				"additionalProperties": { "$ref": "#" },
				"definitions": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"properties": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"patternProperties": {
					"type": "object",
					"regexProperties": true,
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"dependencies": {
					"type": "object",
					"additionalProperties": {
						"anyOf": [
							{ "$ref": "#" },
							{ "$ref": "#/definitions/stringArray" }
						]
					}
				},
				"propertyNames": { "$ref": "#" },
				"const": {},
				"enum": {
					"type": "array",
					"minItems": 1,
					"uniqueItems": true
				},
				"type": {
					"anyOf": [
						{ "$ref": "#/definitions/simpleTypes" },
						{
							"type": "array",
							"items": { "$ref": "#/definitions/simpleTypes" },
							"minItems": 1,
							"uniqueItems": true
						}
					]
				},
				"format": { "type": "string" },
				"allOf": { "$ref": "#/definitions/schemaArray" },
				"anyOf": { "$ref": "#/definitions/schemaArray" },
				"oneOf": { "$ref": "#/definitions/schemaArray" },
				"not": { "$ref": "#" }
			},
			"default": {}
		}`,
	})
	Draft7.loadMeta("http://json-schema.org/draft-07", map[string]string{
		"schema": `{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"$id": "http://json-schema.org/draft-07/schema#",
			"title": "Core schema meta-schema",
			"definitions": {
				"schemaArray": {
					"type": "array",
					"minItems": 1,
					"items": { "$ref": "#" }
				},
				"nonNegativeInteger": {
					"type": "integer",
					"minimum": 0
				},
				"nonNegativeIntegerDefault0": {
					"allOf": [
						{ "$ref": "#/definitions/nonNegativeInteger" },
						{ "default": 0 }
					]
				},
				"simpleTypes": {
					"enum": [
						"array",
						"boolean",
						"integer",
						"null",
						"number",
						"object",
						"string"
					]
				},
				"stringArray": {
					"type": "array",
					"items": { "type": "string" },
					"uniqueItems": true,
					"default": []
				}
			},
			"type": ["object", "boolean"],
			"properties": {
				"$id": {
					"type": "string",
					"format": "uri-reference"
				},
				"$schema": {
					"type": "string",
					"format": "uri"
				},
				"$ref": {
					"type": "string",
					"format": "uri-reference"
				},
				"$comment": {
					"type": "string"
				},
				"title": {
					"type": "string"
				},
				"description": {
					"type": "string"
				},
				"default": true,
				"readOnly": {
					"type": "boolean",
					"default": false
				},
				"writeOnly": {
					"type": "boolean",
					"default": false
				},
				"examples": {
					"type": "array",
					"items": true
				},
				"multipleOf": {
					"type": "number",
					"exclusiveMinimum": 0
				},
				"maximum": {
					"type": "number"
				},
				"exclusiveMaximum": {
					"type": "number"
				},
				"minimum": {
					"type": "number"
				},
				"exclusiveMinimum": {
					"type": "number"
				},
				"maxLength": { "$ref": "#/definitions/nonNegativeInteger" },
				"minLength": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"pattern": {
					"type": "string",
					"format": "regex"
				},
				"additionalItems": { "$ref": "#" },
				"items": {
					"anyOf": [
						{ "$ref": "#" },
						{ "$ref": "#/definitions/schemaArray" }
					],
					"default": true
				},
				"maxItems": { "$ref": "#/definitions/nonNegativeInteger" },
				"minItems": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"uniqueItems": {
					"type": "boolean",
					"default": false
				},
				"contains": { "$ref": "#" },
				"maxProperties": { "$ref": "#/definitions/nonNegativeInteger" },
				"minProperties": { "$ref": "#/definitions/nonNegativeIntegerDefault0" },
				"required": { "$ref": "#/definitions/stringArray" },
				"additionalProperties": { "$ref": "#" },
				"definitions": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"properties": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"default": {}
				},
				"patternProperties": {
					"type": "object",
					"additionalProperties": { "$ref": "#" },
					"propertyNames": { "format": "regex" },
					"default": {}
				},
				"dependencies": {
					"type": "object",
					"additionalProperties": {
						"anyOf": [
							{ "$ref": "#" },
							{ "$ref": "#/definitions/stringArray" }
						]
					}
				},
				"propertyNames": { "$ref": "#" },
				"const": true,
				"enum": {
					"type": "array",
					"items": true,
					"minItems": 1,
					"uniqueItems": true
				},
				"type": {
					"anyOf": [
						{ "$ref": "#/definitions/simpleTypes" },
						{
							"type": "array",
							"items": { "$ref": "#/definitions/simpleTypes" },
							"minItems": 1,
							"uniqueItems": true
						}
					]
				},
				"format": { "type": "string" },
				"contentMediaType": { "type": "string" },
				"contentEncoding": { "type": "string" },
				"if": { "$ref": "#" },
				"then": { "$ref": "#" },
				"else": { "$ref": "#" },
				"allOf": { "$ref": "#/definitions/schemaArray" },
				"anyOf": { "$ref": "#/definitions/schemaArray" },
				"oneOf": { "$ref": "#/definitions/schemaArray" },
				"not": { "$ref": "#" }
			},
			"default": true
		}`,
	})
	Draft2019.loadMeta("https://json-schema.org/draft/2019-09", map[string]string{
		"schema": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/schema",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/core": true,
				"https://json-schema.org/draft/2019-09/vocab/applicator": true,
				"https://json-schema.org/draft/2019-09/vocab/validation": true,
				"https://json-schema.org/draft/2019-09/vocab/meta-data": true,
				"https://json-schema.org/draft/2019-09/vocab/format": false,
				"https://json-schema.org/draft/2019-09/vocab/content": true
			},
			"$recursiveAnchor": true,

			"title": "Core and Validation specifications meta-schema",
			"allOf": [
				{"$ref": "meta/core"},
				{"$ref": "meta/applicator"},
				{"$ref": "meta/validation"},
				{"$ref": "meta/meta-data"},
				{"$ref": "meta/format"},
				{"$ref": "meta/content"}
			],
			"type": ["object", "boolean"],
			"properties": {
				"definitions": {
					"$comment": "While no longer an official keyword as it is replaced by $defs, this keyword is retained in the meta-schema to prevent incompatible extensions as it remains in common use.",
					"type": "object",
					"additionalProperties": { "$recursiveRef": "#" },
					"default": {}
				},
				"dependencies": {
					"$comment": "\"dependencies\" is no longer a keyword, but schema authors should avoid redefining it to facilitate a smooth transition to \"dependentSchemas\" and \"dependentRequired\"",
					"type": "object",
					"additionalProperties": {
						"anyOf": [
							{ "$recursiveRef": "#" },
							{ "$ref": "meta/validation#/$defs/stringArray" }
						]
					}
				}
			}
		}`,
		"meta/core": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/core",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/core": true
			},
			"$recursiveAnchor": true,

			"title": "Core vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"$id": {
					"type": "string",
					"format": "uri-reference",
					"$comment": "Non-empty fragments not allowed.",
					"pattern": "^[^#]*#?$"
				},
				"$schema": {
					"type": "string",
					"format": "uri"
				},
				"$anchor": {
					"type": "string",
					"pattern": "^[A-Za-z][-A-Za-z0-9.:_]*$"
				},
				"$ref": {
					"type": "string",
					"format": "uri-reference"
				},
				"$recursiveRef": {
					"type": "string",
					"format": "uri-reference"
				},
				"$recursiveAnchor": {
					"type": "boolean",
					"default": false
				},
				"$vocabulary": {
					"type": "object",
					"propertyNames": {
						"type": "string",
						"format": "uri"
					},
					"additionalProperties": {
						"type": "boolean"
					}
				},
				"$comment": {
					"type": "string"
				},
				"$defs": {
					"type": "object",
					"additionalProperties": { "$recursiveRef": "#" },
					"default": {}
				}
			}
		}`,
		"meta/applicator": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/applicator",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/applicator": true
			},
			"$recursiveAnchor": true,

			"title": "Applicator vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"additionalItems": { "$recursiveRef": "#" },
				"unevaluatedItems": { "$recursiveRef": "#" },
				"items": {
					"anyOf": [
						{ "$recursiveRef": "#" },
						{ "$ref": "#/$defs/schemaArray" }
					]
				},
				"contains": { "$recursiveRef": "#" },
				"additionalProperties": { "$recursiveRef": "#" },
				"unevaluatedProperties": { "$recursiveRef": "#" },
				"properties": {
					"type": "object",
					"additionalProperties": { "$recursiveRef": "#" },
					"default": {}
				},
				"patternProperties": {
					"type": "object",
					"additionalProperties": { "$recursiveRef": "#" },
					"propertyNames": { "format": "regex" },
					"default": {}
				},
				"dependentSchemas": {
					"type": "object",
					"additionalProperties": {
						"$recursiveRef": "#"
					}
				},
				"propertyNames": { "$recursiveRef": "#" },
				"if": { "$recursiveRef": "#" },
				"then": { "$recursiveRef": "#" },
				"else": { "$recursiveRef": "#" },
				"allOf": { "$ref": "#/$defs/schemaArray" },
				"anyOf": { "$ref": "#/$defs/schemaArray" },
				"oneOf": { "$ref": "#/$defs/schemaArray" },
				"not": { "$recursiveRef": "#" }
			},
			"$defs": {
				"schemaArray": {
					"type": "array",
					"minItems": 1,
					"items": { "$recursiveRef": "#" }
				}
			}
		}`,
		"meta/validation": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/validation",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/validation": true
			},
			"$recursiveAnchor": true,

			"title": "Validation vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"multipleOf": {
					"type": "number",
					"exclusiveMinimum": 0
				},
				"maximum": {
					"type": "number"
				},
				"exclusiveMaximum": {
					"type": "number"
				},
				"minimum": {
					"type": "number"
				},
				"exclusiveMinimum": {
					"type": "number"
				},
				"maxLength": { "$ref": "#/$defs/nonNegativeInteger" },
				"minLength": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"pattern": {
					"type": "string",
					"format": "regex"
				},
				"maxItems": { "$ref": "#/$defs/nonNegativeInteger" },
				"minItems": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"uniqueItems": {
					"type": "boolean",
					"default": false
				},
				"maxContains": { "$ref": "#/$defs/nonNegativeInteger" },
				"minContains": {
					"$ref": "#/$defs/nonNegativeInteger",
					"default": 1
				},
				"maxProperties": { "$ref": "#/$defs/nonNegativeInteger" },
				"minProperties": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"required": { "$ref": "#/$defs/stringArray" },
				"dependentRequired": {
					"type": "object",
					"additionalProperties": {
						"$ref": "#/$defs/stringArray"
					}
				},
				"const": true,
				"enum": {
					"type": "array",
					"items": true
				},
				"type": {
					"anyOf": [
						{ "$ref": "#/$defs/simpleTypes" },
						{
							"type": "array",
							"items": { "$ref": "#/$defs/simpleTypes" },
							"minItems": 1,
							"uniqueItems": true
						}
					]
				}
			},
			"$defs": {
				"nonNegativeInteger": {
					"type": "integer",
					"minimum": 0
				},
				"nonNegativeIntegerDefault0": {
					"$ref": "#/$defs/nonNegativeInteger",
					"default": 0
				},
				"simpleTypes": {
					"enum": [
						"array",
						"boolean",
						"integer",
						"null",
						"number",
						"object",
						"string"
					]
				},
				"stringArray": {
					"type": "array",
					"items": { "type": "string" },
					"uniqueItems": true,
					"default": []
				}
			}
		}`,
		"meta/meta-data": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/meta-data",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/meta-data": true
			},
			"$recursiveAnchor": true,

			"title": "Meta-data vocabulary meta-schema",

			"type": ["object", "boolean"],
			"properties": {
				"title": {
					"type": "string"
				},
				"description": {
					"type": "string"
				},
				"default": true,
				"deprecated": {
					"type": "boolean",
					"default": false
				},
				"readOnly": {
					"type": "boolean",
					"default": false
				},
				"writeOnly": {
					"type": "boolean",
					"default": false
				},
				"examples": {
					"type": "array",
					"items": true
				}
			}
		}`,
		"meta/format": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/format",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/format": true
			},
			"$recursiveAnchor": true,

			"title": "Format vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"format": { "type": "string" }
			}
		}`,
		"meta/content": `{
			"$schema": "https://json-schema.org/draft/2019-09/schema",
			"$id": "https://json-schema.org/draft/2019-09/meta/content",
			"$vocabulary": {
				"https://json-schema.org/draft/2019-09/vocab/content": true
			},
			"$recursiveAnchor": true,

			"title": "Content vocabulary meta-schema",

			"type": ["object", "boolean"],
			"properties": {
				"contentMediaType": { "type": "string" },
				"contentEncoding": { "type": "string" },
				"contentSchema": { "$recursiveRef": "#" }
			}
		}`,
	})
	Draft2020.loadMeta("https://json-schema.org/draft/2020-12", map[string]string{
		"schema": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/schema",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/core": true,
				"https://json-schema.org/draft/2020-12/vocab/applicator": true,
				"https://json-schema.org/draft/2020-12/vocab/unevaluated": true,
				"https://json-schema.org/draft/2020-12/vocab/validation": true,
				"https://json-schema.org/draft/2020-12/vocab/meta-data": true,
				"https://json-schema.org/draft/2020-12/vocab/format-annotation": true,
				"https://json-schema.org/draft/2020-12/vocab/content": true
			},
			"$dynamicAnchor": "meta",

			"title": "Core and Validation specifications meta-schema",
			"allOf": [
				{"$ref": "meta/core"},
				{"$ref": "meta/applicator"},
				{"$ref": "meta/unevaluated"},
				{"$ref": "meta/validation"},
				{"$ref": "meta/meta-data"},
				{"$ref": "meta/format-annotation"},
				{"$ref": "meta/content"}
			],
			"type": ["object", "boolean"],
			"$comment": "This meta-schema also defines keywords that have appeared in previous drafts in order to prevent incompatible extensions as they remain in common use.",
			"properties": {
				"definitions": {
					"$comment": "\"definitions\" has been replaced by \"$defs\".",
					"type": "object",
					"additionalProperties": { "$dynamicRef": "#meta" },
					"deprecated": true,
					"default": {}
				},
				"dependencies": {
					"$comment": "\"dependencies\" has been split and replaced by \"dependentSchemas\" and \"dependentRequired\" in order to serve their differing semantics.",
					"type": "object",
					"additionalProperties": {
						"anyOf": [
							{ "$dynamicRef": "#meta" },
							{ "$ref": "meta/validation#/$defs/stringArray" }
						]
					},
					"deprecated": true,
					"default": {}
				},
				"$recursiveAnchor": {
					"$comment": "\"$recursiveAnchor\" has been replaced by \"$dynamicAnchor\".",
					"$ref": "meta/core#/$defs/anchorString",
					"deprecated": true
				},
				"$recursiveRef": {
					"$comment": "\"$recursiveRef\" has been replaced by \"$dynamicRef\".",
					"$ref": "meta/core#/$defs/uriReferenceString",
					"deprecated": true
				}
			}
		}`,
		"meta/core": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/core",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/core": true
			},
			"$dynamicAnchor": "meta",

			"title": "Core vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"$id": {
					"$ref": "#/$defs/uriReferenceString",
					"$comment": "Non-empty fragments not allowed.",
					"pattern": "^[^#]*#?$"
				},
				"$schema": { "$ref": "#/$defs/uriString" },
				"$ref": { "$ref": "#/$defs/uriReferenceString" },
				"$anchor": { "$ref": "#/$defs/anchorString" },
				"$dynamicRef": { "$ref": "#/$defs/uriReferenceString" },
				"$dynamicAnchor": { "$ref": "#/$defs/anchorString" },
				"$vocabulary": {
					"type": "object",
					"propertyNames": { "$ref": "#/$defs/uriString" },
					"additionalProperties": {
						"type": "boolean"
					}
				},
				"$comment": {
					"type": "string"
				},
				"$defs": {
					"type": "object",
					"additionalProperties": { "$dynamicRef": "#meta" }
				}
			},
			"$defs": {
				"anchorString": {
					"type": "string",
					"pattern": "^[A-Za-z_][-A-Za-z0-9._]*$"
				},
				"uriString": {
					"type": "string",
					"format": "uri"
				},
				"uriReferenceString": {
					"type": "string",
					"format": "uri-reference"
				}
			}
		}`,
		"meta/applicator": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/applicator",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/applicator": true
			},
			"$dynamicAnchor": "meta",

			"title": "Applicator vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"prefixItems": { "$ref": "#/$defs/schemaArray" },
				"items": { "$dynamicRef": "#meta" },
				"contains": { "$dynamicRef": "#meta" },
				"additionalProperties": { "$dynamicRef": "#meta" },
				"properties": {
					"type": "object",
					"additionalProperties": { "$dynamicRef": "#meta" },
					"default": {}
				},
				"patternProperties": {
					"type": "object",
					"additionalProperties": { "$dynamicRef": "#meta" },
					"propertyNames": { "format": "regex" },
					"default": {}
				},
				"dependentSchemas": {
					"type": "object",
					"additionalProperties": { "$dynamicRef": "#meta" },
					"default": {}
				},
				"propertyNames": { "$dynamicRef": "#meta" },
				"if": { "$dynamicRef": "#meta" },
				"then": { "$dynamicRef": "#meta" },
				"else": { "$dynamicRef": "#meta" },
				"allOf": { "$ref": "#/$defs/schemaArray" },
				"anyOf": { "$ref": "#/$defs/schemaArray" },
				"oneOf": { "$ref": "#/$defs/schemaArray" },
				"not": { "$dynamicRef": "#meta" }
			},
			"$defs": {
				"schemaArray": {
					"type": "array",
					"minItems": 1,
					"items": { "$dynamicRef": "#meta" }
				}
			}
		}`,
		"meta/unevaluated": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/unevaluated",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/unevaluated": true
			},
			"$dynamicAnchor": "meta",

			"title": "Unevaluated applicator vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"unevaluatedItems": { "$dynamicRef": "#meta" },
				"unevaluatedProperties": { "$dynamicRef": "#meta" }
			}
		}`,
		"meta/validation": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/validation",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/validation": true
			},
			"$dynamicAnchor": "meta",

			"title": "Validation vocabulary meta-schema",
			"type": ["object", "boolean"],
			"properties": {
				"type": {
					"anyOf": [
						{ "$ref": "#/$defs/simpleTypes" },
						{
							"type": "array",
							"items": { "$ref": "#/$defs/simpleTypes" },
							"minItems": 1,
							"uniqueItems": true
						}
					]
				},
				"const": true,
				"enum": {
					"type": "array",
					"items": true
				},
				"multipleOf": {
					"type": "number",
					"exclusiveMinimum": 0
				},
				"maximum": {
					"type": "number"
				},
				"exclusiveMaximum": {
					"type": "number"
				},
				"minimum": {
					"type": "number"
				},
				"exclusiveMinimum": {
					"type": "number"
				},
				"maxLength": { "$ref": "#/$defs/nonNegativeInteger" },
				"minLength": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"pattern": {
					"type": "string",
					"format": "regex"
				},
				"maxItems": { "$ref": "#/$defs/nonNegativeInteger" },
				"minItems": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"uniqueItems": {
					"type": "boolean",
					"default": false
				},
				"maxContains": { "$ref": "#/$defs/nonNegativeInteger" },
				"minContains": {
					"$ref": "#/$defs/nonNegativeInteger",
					"default": 1
				},
				"maxProperties": { "$ref": "#/$defs/nonNegativeInteger" },
				"minProperties": { "$ref": "#/$defs/nonNegativeIntegerDefault0" },
				"required": { "$ref": "#/$defs/stringArray" },
				"dependentRequired": {
					"type": "object",
					"additionalProperties": {
						"$ref": "#/$defs/stringArray"
					}
				}
			},
			"$defs": {
				"nonNegativeInteger": {
					"type": "integer",
					"minimum": 0
				},
				"nonNegativeIntegerDefault0": {
					"$ref": "#/$defs/nonNegativeInteger",
					"default": 0
				},
				"simpleTypes": {
					"enum": [
						"array",
						"boolean",
						"integer",
						"null",
						"number",
						"object",
						"string"
					]
				},
				"stringArray": {
					"type": "array",
					"items": { "type": "string" },
					"uniqueItems": true,
					"default": []
				}
			}
		}`,
		"meta/meta-data": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/meta-data",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/meta-data": true
			},
			"$dynamicAnchor": "meta",

			"title": "Meta-data vocabulary meta-schema",

			"type": ["object", "boolean"],
			"properties": {
				"title": {
					"type": "string"
				},
				"description": {
					"type": "string"
				},
				"default": true,
				"deprecated": {
					"type": "boolean",
					"default": false
				},
				"readOnly": {
					"type": "boolean",
					"default": false
				},
				"writeOnly": {
					"type": "boolean",
					"default": false
				},
				"examples": {
					"type": "array",
					"items": true
				}
			}
		}`,
		"meta/format-annotation": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/format-annotation",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/format-annotation": true
			},
			"$dynamicAnchor": "meta",

			"title": "Format vocabulary meta-schema for annotation results",
			"type": ["object", "boolean"],
			"properties": {
				"format": { "type": "string" }
			}
		}`,
		"meta/content": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"$id": "https://json-schema.org/draft/2020-12/meta/content",
			"$vocabulary": {
				"https://json-schema.org/draft/2020-12/vocab/content": true
			},
			"$dynamicAnchor": "meta",

			"title": "Content vocabulary meta-schema",

			"type": ["object", "boolean"],
			"properties": {
				"contentEncoding": { "type": "string" },
				"contentMediaType": { "type": "string" },
				"contentSchema": { "$dynamicRef": "#meta" }
			}
		}`,
	})
}

func clone(m map[string]position) map[string]position {
	mm := make(map[string]position)
	for k, v := range m {
		mm[k] = v
	}
	return mm
}
