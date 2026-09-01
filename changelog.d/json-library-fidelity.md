### Fixed

- **`json_library`'s declaration now means what `resolveJsonModule` means for
  the same file.** Two divergences, both silent. An array's element type was
  sampled from `v[0]`, so every key only later elements carried was erased and
  every key the first one had was marked required — a 96-entry file with 14
  distinct shapes was typed as the 8 keys of its first entry. And every
  property and array was `readonly`, which rejects assignments the same import
  accepts under `resolveJsonModule`. An array's element type is now the union
  of its elements, normalised the way TypeScript normalises a union of object
  literals — an absent key becomes `?: undefined`, so it stays readable rather
  than vanishing — and nothing is `readonly`. An empty array is `never[]` and
  an empty object is `{}`, matching what TypeScript infers for both.
