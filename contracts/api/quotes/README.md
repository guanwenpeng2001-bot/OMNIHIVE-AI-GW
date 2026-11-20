# BE-07 quote fixtures

The JSON files contain the request and the unwrapped `data` for a project quote. The half-up charge is USD `0.00007813` from exact raw USD `0.000078125`. Each component includes the exact numerator and denominator plus an 18-place diagnostic decimal. An unpriced estimate has no settled amount or price version. A free estimate has an explicit eight-place zero amount.

Scenario quantities apply per request. Cached input is a subset of input. Other dimensions are disjoint, and both context size and aggregate dimension bounds are checked. A comparison preserves the BE-05 output shape and uses a single catalog snapshot for the selected rows. Unknown capabilities and metrics remain unknown, and benchmarks require a verified observation.
