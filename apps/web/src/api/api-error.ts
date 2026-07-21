export class ApiError extends Error {
  name = "ApiError";
  constructor(
    public status: number,
    public body?: unknown,
  ) {
    super(`HTTP ${status}`);
  }
}

export class NetworkError extends Error {
  name = "NetworkError";
}

export class UnauthorizedError extends ApiError {
  name = "UnauthorizedError";
  constructor(body?: unknown) {
    super(401, body);
  }
}

export class ForbiddenError extends ApiError {
  name = "ForbiddenError";
  constructor(body?: unknown) {
    super(403, body);
  }
}
