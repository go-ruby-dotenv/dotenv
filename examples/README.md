# Examples

Runnable pure-Ruby usage of `dotenv`, verified under the [rbgo](https://github.com/go-embedded-ruby/ruby) interpreter.

```sh
rbgo examples/dotenv_usage.rb
```

| File | Shows |
| --- | --- |
| `dotenv_usage.rb` | `Dotenv.parse` into an ordered Hash, `$VAR` / `${VAR}` interpolation, literal single-quoted values, and `Dotenv.load` / `Dotenv.overload` setting keys into `ENV`. |
