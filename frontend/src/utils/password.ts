/**
 * Password strength utility functions
 */

export interface PasswordStrength {
  level: 'weak' | 'medium' | 'strong'
  text: string
}

/**
 * Check if password contains uppercase letters
 */
const hasUpperCase = (str: string): boolean => /[A-Z]/.test(str)

/**
 * Check if password contains numbers
 */
const hasNumber = (str: string): boolean => /[0-9]/.test(str)

/**
 * Check if password contains special characters
 */
const hasSpecialChar = (str: string): boolean => /[^A-Za-z0-9]/.test(str)

/**
 * Check password strength
 * @param password - The password to check
 * @returns Password strength information
 */
export function checkPasswordStrength(password: string): PasswordStrength {
  if (password.length < 6) {
    return { level: 'weak', text: '弱' }
  } else if (password.length < 10) {
    return { level: 'medium', text: '中等' }
  } else if (hasUpperCase(password) && hasNumber(password) && hasSpecialChar(password)) {
    return { level: 'strong', text: '强' }
  } else {
    return { level: 'medium', text: '中等' }
  }
}
