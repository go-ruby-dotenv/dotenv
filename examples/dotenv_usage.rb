# frozen_string_literal: true

require "dotenv"

# The `.env` source is just a String here; nothing is read from disk.
source = <<~ENV
  # Application settings
  APP_NAME=Widgets
  export PORT=8080
  GREETING="Hello, ${APP_NAME}"
  LITERAL='no $interpolation here'
ENV

# Dotenv.parse returns an insertion-ordered Hash without touching ENV.
config = Dotenv.parse(source)
config.each { |key, value| puts "#{key}=#{value}" }

# $VAR / ${VAR} interpolation references earlier keys.
puts config["GREETING"]           # => Hello, Widgets
puts config["LITERAL"]            # => no $interpolation here (single quotes are literal)

# Dotenv.load parses AND sets each pair into ENV, keeping existing keys;
# Dotenv.overload overwrites them instead.
Dotenv.load("TOKEN=abc123")
puts ENV["TOKEN"]                 # => abc123
Dotenv.overload("TOKEN=xyz789")
puts ENV["TOKEN"]                 # => xyz789
