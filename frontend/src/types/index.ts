export type Role = 'USER' | 'ADMIN'
export interface User { id: string; email: string; display_name: string; avatar_url?: string; role: Role }
export interface Movie { id: string; title: string; description: string; duration_minutes: number; poster_url: string; status: string }
export interface Showtime { id: string; movie_id: string; auditorium_id: string; start_time: string; end_time: string; status: string }
export interface Auditorium { id: string; name: string; rows: number; seats_per_row: number }
export interface ShowtimeDetail { showtime: Showtime; movie: Movie; auditorium: Auditorium }
export type SeatStatus = 'AVAILABLE' | 'LOCKED' | 'BOOKED'
export interface Seat { id: string; showtime_id: string; seat_id: string; seat_label: string; row: string; number: number; price: number; status: SeatStatus; lock_expires_at?: string }
export interface Hold { hold_id: string; showtime_id: string; seat_ids: string[]; status: 'ACTIVE' | 'CONFIRMED' | 'EXPIRED' | 'RELEASED'; expires_at: string; remaining_seconds: number }
export interface BookingSeat { seat_id: string; label: string; price: number }
export interface Booking { id: string; booking_number: string; hold_id: string; user_id: string; user_email: string; showtime_id: string; movie_id: string; movie_title: string; showtime_start: string; auditorium_name: string; seats: BookingSeat[]; total_amount: number; currency: string; payment_status: string; booking_status: string; created_at: string }
export interface AuditLog { id: string; event_type: string; entity_type: string; entity_id: string; severity: string; metadata?: Record<string, unknown>; created_at: string }
export interface Envelope<T> { data: T; meta: { request_id: string; page?: number; limit?: number; total?: number } }

