// Package validate is a generic go data validate, filtering library.
//
// Source code and other details for the project are available at GitHub:
//
// 	https://github.com/gookit/validate
//
package validate

import (
	"reflect"
)

// type sourceType uint8
// const requiredValidator = "required"

// const (
// from user setting, unmarshal JSON
// sourceMap sourceType = iota + 1
// from URL.Values, PostForm. contains Files data
// sourceForm
// from user setting
// sourceStruct
// )

// Apply current rule for the rule fields
func (r *Rule) Apply(v *Validation) (stop bool) {
	// scene name is not match. skip the rule
	if r.scene != "" && r.scene != v.scene {
		return false
	}

	var err error
	name := ValidatorName(r.validator)

	// validate each field
	for _, field := range r.fields {
		if v.isNoNeedToCheck(field) {
			continue
		}

		// has beforeFunc. if return false, skip validate
		if r.beforeFunc != nil && !r.beforeFunc(field, v) {
			continue
		}

		// uploaded file check
		if isFileValidator(name) {
			// build and collect error message
			if !r.fileValidate(field, name, v) {
				v.AddError(field, r.errorMessage(field, r.validator, v))
				// stop on error
				if v.StopOnError {
					return true
				}
			}
			continue
		}

		// get field value.
		val, exist := v.Get(field)
		// not exist AND r.optional=true. skip check.
		if !exist && r.optional {
			continue
		}

		// apply filter func.
		if exist && r.filterFunc != nil {
			if val, err = r.filterFunc(val); err != nil { // has error
				v.AddError(filterError, err.Error())
				return true
			}

			// save filtered value.
			v.filteredData[field] = val
		}

		if r.valueValidate(field, name, val, v) {
			v.safeData[field] = val // save validated value.
		} else { // build and collect error message
			v.AddError(field, r.errorMessage(field, r.validator, v))
		}

		// stop on error
		if v.shouldStop() {
			return true
		}
	}

	return false
}

func (r *Rule) fileValidate(field, name string, v *Validation) (ok bool) {
	// check data source
	form, ok := v.data.(*FormData)
	if !ok {
		return
	}

	// skip on empty AND field not exist
	if r.skipEmpty && !form.HasFile(field) {
		return true
	}

	var ss []string
	for _, item := range r.arguments {
		ss = append(ss, item.(string))
	}

	switch name {
	case "isFile":
		ok = v.IsFile(form, field)
	case "isImage":
		ok = v.IsImage(form, field, ss...)
	case "inMimeTypes":
		ln := len(ss)
		if ln == 0 {
			return false
		} else if ln == 1 {
			//noinspection GoNilness
			ok = v.InMimeTypes(form, field, ss[0])
		} else { // ln > 1
			//noinspection GoNilness
			ok = v.InMimeTypes(form, field, ss[0], ss[1:]...)
		}
	}
	return
}

// validate the field value
func (r *Rule) valueValidate(field, name string, val interface{}, v *Validation) (ok bool) {
	// "-" OR "safe" mark field value always is safe.
	if name == "-" || name == "safe" {
		return true
	}

	// empty value AND skip on empty.
	isNotRequired := name != "required"
	if r.skipEmpty && isNotRequired && IsEmpty(val) {
		return true
	}

	// call custom validator in the rule.
	fm := r.checkFuncMeta
	if fm == nil {
		// get validator for global or validation
		fm = v.validatorMeta(name)
		if fm == nil {
			panicf("the validator '%s' is not exists", r.validator)
		}
	}

	// some prepare and check.
	argNum := len(r.arguments) + 1 // "+1" is the "val" position
	rftVal := reflect.ValueOf(val)
	valKind := rftVal.Kind()
	// check arg num is match
	if isNotRequired { // need exclude "required"
		//noinspection GoNilness
		fm.checkArgNum(argNum, r.validator)

		// convert val type, is first arg.
		//noinspection GoNilness
		ft := fm.fv.Type()
		firstTyp := ft.In(0).Kind()
		if firstTyp != valKind && firstTyp != reflect.Interface {
			ak, err := basicKind(rftVal)
			if err != nil { // todo check?
				//noinspection GoNilness
				v.convertArgTypeError(fm.name, valKind, firstTyp)
				return
			}

			// manual converted
			if nVal, _ := convertType(val, ak, firstTyp); nVal != nil {
				val = nVal
			}
		}
	}

	// call built in validators
	ok = callValidator(v, fm, field, val, r.arguments)
	return
}

// convert args data type
func convertArgsType(v *Validation, fm *funcMeta, args []interface{}) (ok bool) {
	ft := fm.fv.Type()
	lastTyp := reflect.Invalid
	lastArgIndex := fm.numIn - 1

	// isVariadic == true. last arg always is slice.
	// eg. "...int64" -> slice "[]int64"
	if fm.isVariadic {
		// get variadic kind. "[]int64" -> reflect.Int64
		lastTyp = getSliceItemKind(ft.In(lastArgIndex).String())
	}

	var wantTyp reflect.Kind

	// convert args data type
	for i, arg := range args {
		av := reflect.ValueOf(arg)

		// "+1" because first arg is val, need exclude it.
		if fm.isVariadic && i+1 >= lastArgIndex {
			if lastTyp == av.Kind() { // type is same
				continue
			}

			ak, err := basicKind(av)
			if err != nil {
				v.convertArgTypeError(fm.name, av.Kind(), wantTyp)
				return
			}

			// manual converted
			if nVal, _ := convertType(args[i], ak, lastTyp); nVal != nil {
				args[i] = nVal
				continue
			}

			// unable to convert
			v.convertArgTypeError(fm.name, av.Kind(), wantTyp)
			return
		}

		// "+1" because func first arg is val, need skip it.
		argITyp := ft.In(i + 1)
		wantTyp = argITyp.Kind()

		// type is same. or want type is interface
		if wantTyp == av.Kind() || wantTyp == reflect.Interface {
			continue
		}

		ak, err := basicKind(av)
		if err != nil {
			v.convertArgTypeError(fm.name, av.Kind(), wantTyp)
			return
		}

		if av.Type().ConvertibleTo(argITyp) { // can auto convert type.
			args[i] = av.Convert(argITyp).Interface()
		} else if nVal, _ := convertType(args[i], ak, wantTyp); nVal != nil { // manual converted
			args[i] = nVal
		} else { // unable to convert
			v.convertArgTypeError(fm.name, av.Kind(), wantTyp)
			return
		}
	}

	return true
}

func callValidator(v *Validation, fm *funcMeta, field string, val interface{}, args []interface{}) (ok bool) {
	// 1. args data type convert
	if ok = convertArgsType(v, fm, args); !ok {
		return
	}

	// 2. call built in validator
	switch fm.name {
	case "required":
		ok = v.Required(field, val)
	case "lt":
		ok = Lt(val, args[0].(int64))
	case "gt":
		ok = Gt(val, args[0].(int64))
	case "min":
		ok = Min(val, args[0].(int64))
	case "max":
		ok = Max(val, args[0].(int64))
	case "enum":
		ok = Enum(val, args[0])
	case "notIn":
		ok = NotIn(val, args[0])
	case "isInt":
		if argLn := len(args); argLn == 0 {
			ok = IsInt(val)
		} else if argLn == 1 {
			ok = IsInt(val, args[0].(int64))
		} else { // argLn == 2
			ok = IsInt(val, args[0].(int64), args[1].(int64))
		}
	case "isString":
		if argLn := len(args); argLn == 0 {
			ok = IsString(val)
		} else if argLn == 1 {
			ok = IsString(val, args[0].(int))
		} else { // argLn == 2
			ok = IsString(val, args[0].(int), args[1].(int))
		}
	case "isNumber":
		ok = IsNumber(val.(string))
	case "length":
		ok = Length(val, args[0].(int))
	case "minLength":
		ok = MinLength(val, args[0].(int))
	case "maxLength":
		ok = MaxLength(val, args[0].(int))
	case "stringLength":
		if argLn := len(args); argLn == 1 {
			ok = RuneLength(val, args[0].(int))
		} else if argLn == 2 {
			ok = RuneLength(val, args[0].(int), args[1].(int))
		}
	case "regexp":
		ok = Regexp(val.(string), args[0].(string))
	case "between":
		ok = Between(val, args[0].(int64), args[1].(int64))
	case "isJSON":
		ok = IsJSON(val.(string))
	default:
		// 3. call user custom validators, will call by reflect
		ok = callValidatorValue(fm.fv, val, args)
	}
	return
}

func callValidatorValue(fv reflect.Value, val interface{}, args []interface{}) bool {
	argNum := len(args)

	// build params for the validator func.
	argIn := make([]reflect.Value, argNum+1)
	argIn[0] = reflect.ValueOf(val)

	for i := 0; i < argNum; i++ {
		argIn[i+1] = reflect.ValueOf(args[i])
	}

	// NOTICE: f.CallSlice()与Call() 不一样的是，CallSlice参数的最后一个会被展开
	// vs := fv.Call(argIn)
	return fv.Call(argIn)[0].Bool()
}
