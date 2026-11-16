import { defineEventHandler } from "h3";
import { useRuntimeConfig } from "#imports";
import { removeRating } from "../../bff/handlers";

export default defineEventHandler((event) => removeRating(event, useRuntimeConfig(event)));
