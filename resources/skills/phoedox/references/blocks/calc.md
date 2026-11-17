# ```calc – calculators

One line per value, `Label (unit) = …`. A number is an input readers can change,
`@entry.attr` takes an entry's attribute as input, anything else is a formula. Formulas
use other values by their label with spaces as `_` and without the unit
(`Hours per day (h)` → `Hours_per_day`), `+ - * / ^ %`, comparisons, `&` (joins text) and
SUM, AVG, MIN, MAX, ROUND(x; digits), CEIL, FLOOR, ABS, SQRT, IF(cond; a; b). Decimal
comma or point both work. Nothing is saved.

````markdown
```calc
title: Power cost of the NAS
Power (W) = @nas.power_w
Price (€/kWh) = 0.32
Cost per year (€) = Power * 24 * 365 / 1000 * Price
Cost per month (€) = Cost_per_year / 12
```
````

`validate_blocks` checks it; `create_document` and `update_document` return the same findings as `blockProblems`. (unknown names, cycles, broken formulas). On whiteboards it can be used
right on the board.
