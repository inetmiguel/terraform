package providers

import (
	"fmt"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"

	"github.com/hashicorp/terraform/internal/configs/configschema"
)

type FunctionDecl struct {
	Parameters        []FunctionParam
	VariadicParameter *FunctionParam
	ReturnType        cty.Type

	Description     string
	DescriptionKind configschema.StringKind
}

type FunctionParam struct {
	Name string // Only for documentation and UI, because arguments are positional
	Type cty.Type

	Nullable           bool
	AllowUnknownValues bool

	Description     string
	DescriptionKind configschema.StringKind
}

// BuildFunction takes a factory function which will return an unconfigured
// instance of the provider this declaration belongs to and returns a
// cty function that is ready to be called against that provider.
//
// The given name must be the name under which the provider originally
// registered this declaration, or the returned function will try to use an
// invalid name, leading to errors or undefined behavior.
//
// If the given factory returns an instance of any provider other than the
// one the declaration belongs to, or returns a _configured_ instance of
// the provider rather than an unconfigured one, the behavior of the returned
// function is undefined.
//
// Although not functionally required, callers should ideally pass a factory
// function that either retrieves already-running plugins or memoizes the
// plugins it returns so that many calls to functions in the same provider
// will not incur a repeated startup cost.
func (d *FunctionDecl) BuildFunction(name string, factory func() (Interface, error)) function.Function {

	var params []function.Parameter
	var varParam *function.Parameter
	if len(d.Parameters) > 0 {
		params = make([]function.Parameter, len(d.Parameters))
		for i, paramDecl := range d.Parameters {
			params[i] = paramDecl.ctyParameter()
		}
	}
	if d.VariadicParameter != nil {
		cp := d.VariadicParameter.ctyParameter()
		varParam = &cp
	}

	argParamDecl := func(idx int) *FunctionParam {
		if idx < len(d.Parameters) {
			return &d.Parameters[idx]
		}
		return d.VariadicParameter
	}

	return function.New(&function.Spec{
		Type:     function.StaticReturnType(d.ReturnType),
		Params:   params,
		VarParam: varParam,
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			// We promise provider developers that we won't pass them even
			// _nested_ unknown values unless they opt in to dealing with them.
			for i, arg := range args {
				if !argParamDecl(i).AllowUnknownValues {
					if !arg.IsWhollyKnown() {
						return cty.UnknownVal(retType), nil
					}
				}
			}

			provider, err := factory()
			if err != nil {
				return cty.UnknownVal(retType), fmt.Errorf("failed to launch provider plugin: %s", err)
			}

			resp := provider.CallFunction(CallFunctionRequest{
				FunctionName: name,
				Arguments:    args,
			})
			// NOTE: We don't actually have any way to surface warnings
			// from the function here, because functions just return normal
			// Go errors rather than diagnostics.
			if resp.Diagnostics.HasErrors() {
				return cty.UnknownVal(retType), resp.Diagnostics.Err()
			}

			if resp.Result == cty.NilVal {
				return cty.UnknownVal(retType), fmt.Errorf("provider returned no result and no errors")
			}

			err = provider.Close()
			if err != nil {
				return cty.UnknownVal(retType), fmt.Errorf("failed to terminate provider plugin: %s", err)
			}

			return resp.Result, nil
		},
	})
}

func (p *FunctionParam) ctyParameter() function.Parameter {
	return function.Parameter{
		Name:      p.Name,
		Type:      p.Type,
		AllowNull: p.Nullable,

		// NOTE: Setting this is not a sufficient implementation of
		// FunctionParam.AllowUnknownValues, because cty's function
		// system only blocks passing in a top-level unknown, but
		// our provider-contributed functions API promises to only
		// pass wholly-known values unless AllowUnknownValues is true.
		// The function implementation itself must also check this.
		AllowUnknown: p.AllowUnknownValues,
	}
}
