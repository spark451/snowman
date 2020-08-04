# Snowblower

A simple library for modeling snowplow analytics for use with snowblower:

- You define the query and the library tracks the last ETLTimestamp since your last request
- Pipe results to your custom function or a queue
- Pull results from the DB or a queue
