export class ApiError extends Error {
  name = "ApiError"
  status: number
  body?: unknown

  constructor(status: number, body?: unknown) {
    super(`HTTP ${status}`)
    this.status = status
    this.body = body
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
